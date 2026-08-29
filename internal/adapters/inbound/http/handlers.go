package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/application/usecases"
	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/laborview"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// Handlers holds every use case the inbound HTTP adapter depends on.
type Handlers struct {
	ReceiveChargeForecast *usecases.ReceiveChargeForecast
	CommitShiftPlan       *usecases.CommitShiftPlan
	EnqueueWorkUnit       *usecases.EnqueueWorkUnit
	ReleaseNextWork       *usecases.ReleaseNextWork
	RecordCompletion      *usecases.RecordCompletion
	SampleBacklog         *usecases.SampleBacklog
	RebalanceDecision     *usecases.RebalanceDecision

	// Additive: cross-service integration read models (Task 7).
	LaborPlanView *usecases.LaborPlanView
	InventoryView *usecases.InventoryView

	// Additive: read-only lookup of work units by their order-line
	// reference, for cross-service console screens.
	GetWorkUnitsByReference *usecases.GetWorkUnitsByReference
}

func pathIdParam(r *http.Request) (shared.PathId, error) {
	return shared.NewPathId(chi.URLParam(r, "pathId"))
}

// decodeJSON decodes the request body into dst, writing an RFC-mapped 400
// response and returning false on malformed JSON so callers can bail out.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, r, fmt.Errorf("%w: %v", errMalformedBody, err))
		return false
	}
	return true
}

func toBucketDTOs(buckets []charge.CPTBucket) []cptBucketDTO {
	out := make([]cptBucketDTO, len(buckets))
	for i, b := range buckets {
		out[i] = cptBucketDTO{CPT: b.CPT.Time(), Quantity: b.Quantity.Value()}
	}
	return out
}

func toWorkUnitResponseDTO(unit *workunit.WorkUnit) workUnitResponseDTO {
	return workUnitResponseDTO{
		Id:          unit.Id(),
		PathId:      unit.PathId().String(),
		CPT:         unit.CPT().Time(),
		Reference:   unit.Reference(),
		State:       unit.State().String(),
		GiftWrap:    unit.GiftWrap(),
		SKU:         unit.SKU(),
		ReleasedAt:  unit.ReleasedAt(),
		CompletedAt: unit.CompletedAt(),
	}
}

func (h *Handlers) postChargeForecast(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	var body receiveChargeForecastRequestDTO
	if !decodeJSON(w, r, &body) {
		return
	}

	buckets := make([]usecases.CPTBucketInput, len(body.Buckets))
	for i, b := range body.Buckets {
		qty, err := shared.NewQuantity(b.Quantity)
		if err != nil {
			writeError(w, r, err)
			return
		}
		buckets[i] = usecases.CPTBucketInput{CPT: shared.NewCPT(b.CPT), Quantity: qty}
	}

	forecast, err := h.ReceiveChargeForecast.Execute(r.Context(), usecases.ReceiveChargeForecastRequest{
		PathId:  pathId,
		Buckets: buckets,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	w.Header().Set("Location", "/paths/"+forecast.PathId().String()+"/charge")
	writeJSON(w, http.StatusCreated, chargeForecastResponseDTO{
		PathId:        forecast.PathId().String(),
		TotalQuantity: forecast.TotalQuantity().Value(),
		Buckets:       toBucketDTOs(forecast.Buckets()),
		ReceivedAt:    forecast.ReceivedAt(),
	})
}

func (h *Handlers) postShiftPlan(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	var body commitShiftPlanRequestDTO
	if !decodeJSON(w, r, &body) {
		return
	}

	heads, err := shared.NewStationCount(body.PlannedHeads)
	if err != nil {
		writeError(w, r, err)
		return
	}
	installed, err := shared.NewStationCount(body.InstalledStations)
	if err != nil {
		writeError(w, r, err)
		return
	}
	rate, err := shared.NewRate(body.RateUnitsPerHour)
	if err != nil {
		writeError(w, r, err)
		return
	}

	shiftPlan, err := h.CommitShiftPlan.Execute(r.Context(), usecases.CommitShiftPlanRequest{
		PathId:            pathId,
		PlannedHeads:      heads,
		InstalledStations: installed,
		Rate:              rate,
		Hours:             body.Hours,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	pathPlan, _ := shiftPlan.PathPlan(pathId)
	w.Header().Set("Location", "/paths/"+pathId.String()+"/plan")
	writeJSON(w, http.StatusCreated, toShiftPlanResponseDTO(pathPlan))
}

func toShiftPlanResponseDTO(p plan.PathPlan) shiftPlanResponseDTO {
	return shiftPlanResponseDTO{
		PathId:            p.PathId().String(),
		PlannedHeads:      p.PlannedHeads().Value(),
		InstalledStations: p.InstalledStations().Value(),
		RateUnitsPerHour:  p.Rate().UnitsPerHour(),
		Hours:             p.Hours(),
		PlannedThroughput: p.PlannedThroughput(),
	}
}

func (h *Handlers) postWorkUnit(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	var body enqueueWorkUnitRequestDTO
	if !decodeJSON(w, r, &body) {
		return
	}

	unit, err := h.EnqueueWorkUnit.Execute(r.Context(), usecases.EnqueueWorkUnitRequest{
		WorkUnitId: body.WorkUnitId,
		PathId:     pathId,
		CPT:        shared.NewCPT(body.CPT),
		Reference:  body.Reference,
		SKU:        body.SKU,
		GiftWrap:   body.GiftWrap,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	w.Header().Set("Location", "/work-units/"+unit.Id())
	writeJSON(w, http.StatusCreated, toWorkUnitResponseDTO(unit))
}

func (h *Handlers) postRelease(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	unit, err := h.ReleaseNextWork.Execute(r.Context(), usecases.ReleaseNextWorkRequest{PathId: pathId})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toWorkUnitResponseDTO(unit))
}

func (h *Handlers) postComplete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	unit, err := h.RecordCompletion.Execute(r.Context(), usecases.RecordCompletionRequest{WorkUnitId: id})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toWorkUnitResponseDTO(unit))
}

func (h *Handlers) getTelemetry(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	snapshot, err := h.SampleBacklog.Execute(r.Context(), usecases.SampleBacklogRequest{PathId: pathId})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, backlogSnapshotResponseDTO{
		PathId:             snapshot.PathId.String(),
		BacklogDepth:       snapshot.BacklogDepth,
		WIP:                snapshot.WIP,
		Mode:               snapshot.Mode,
		OverAlarmThreshold: snapshot.OverAlarmThreshold,
	})
}

func (h *Handlers) getRebalance(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	rec, err := h.RebalanceDecision.Execute(r.Context(), usecases.RebalanceDecisionRequest{PathId: pathId})
	if err != nil {
		writeError(w, r, err)
		return
	}

	resp := rebalanceResponseDTO{
		PathId:       rec.PathId.String(),
		Action:       rec.Action.String(),
		BacklogDepth: rec.BacklogDepth,
		WIP:          rec.WIP,
	}
	if h.LaborPlanView != nil {
		if view, err := h.LaborPlanView.Execute(r.Context(), usecases.LaborPlanViewRequest{PathId: pathId}); err == nil {
			resp.LaborPlan = toLaborPlanViewDTO(view)
		} else if !errors.Is(err, ports.ErrNotFound) {
			writeError(w, r, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func toLaborPlanViewDTO(view laborview.LaborPlanObserved) *laborPlanViewDTO {
	return &laborPlanViewDTO{
		PathId:       view.PathId.String(),
		PlannedHeads: view.PlannedHeads,
		PlannedRate:  view.PlannedRate,
		PlannedHours: view.PlannedHours,
		ObservedAt:   view.ObservedAt,
	}
}

func (h *Handlers) getLaborPlanView(w http.ResponseWriter, r *http.Request) {
	pathId, err := pathIdParam(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	view, err := h.LaborPlanView.Execute(r.Context(), usecases.LaborPlanViewRequest{PathId: pathId})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toLaborPlanViewDTO(view))
}

func (h *Handlers) getInventoryView(w http.ResponseWriter, r *http.Request) {
	sku := chi.URLParam(r, "sku")

	view, err := h.InventoryView.Execute(r.Context(), usecases.InventoryViewRequest{SKU: sku})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, inventoryViewResponseDTO{
		SKU:            view.SKU,
		UsableQuantity: view.UsableQuantity,
		ObservedAt:     view.ObservedAt,
	})
}

func healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) getWorkUnitsByReference(w http.ResponseWriter, r *http.Request) {
	reference := r.URL.Query().Get("reference")
	if reference == "" {
		writeError(w, r, errMissingReference)
		return
	}

	units, err := h.GetWorkUnitsByReference.Execute(r.Context(), usecases.GetWorkUnitsByReferenceRequest{Reference: reference})
	if err != nil {
		writeError(w, r, err)
		return
	}

	out := make([]workUnitResponseDTO, len(units))
	for i, unit := range units {
		out[i] = toWorkUnitResponseDTO(unit)
	}

	writeJSON(w, http.StatusOK, out)
}
