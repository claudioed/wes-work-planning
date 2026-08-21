package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/claudioed/wes-work-planning/internal/application/ports"
	"github.com/claudioed/wes-work-planning/internal/domain/charge"
	"github.com/claudioed/wes-work-planning/internal/domain/plan"
	"github.com/claudioed/wes-work-planning/internal/domain/release"
	"github.com/claudioed/wes-work-planning/internal/domain/shared"
	"github.com/claudioed/wes-work-planning/internal/domain/workunit"
)

// statusFor maps a domain/application error to an HTTP status code.
func statusFor(err error) int {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, release.ErrWIPLimitReached),
		errors.Is(err, release.ErrAlreadyReleased),
		errors.Is(err, release.ErrDuplicateEntry),
		errors.Is(err, release.ErrEmptyPool),
		errors.Is(err, workunit.ErrAlreadyReleased),
		errors.Is(err, workunit.ErrAlreadyCompleted),
		errors.Is(err, workunit.ErrNotReleased):
		return http.StatusConflict
	case errors.Is(err, shared.ErrInvalidQuantity),
		errors.Is(err, shared.ErrInvalidRate),
		errors.Is(err, shared.ErrInvalidStationCount),
		errors.Is(err, shared.ErrInvalidPathId),
		errors.Is(err, shared.ErrInvalidHours),
		errors.Is(err, charge.ErrNoBuckets),
		errors.Is(err, charge.ErrUnknownCPT),
		errors.Is(err, plan.ErrHeadsExceedStations),
		errors.Is(err, plan.ErrNoPathPlans),
		errors.Is(err, workunit.ErrEmptyId),
		errors.Is(err, workunit.ErrEmptyReference),
		errors.Is(err, release.ErrUnknownEntry):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := statusFor(err)
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
