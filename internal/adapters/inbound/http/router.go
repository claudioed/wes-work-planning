package http

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter wires every REST endpoint to its handler.
func NewRouter(h *Handlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", healthz)

	r.Route("/paths/{pathId}", func(r chi.Router) {
		r.Post("/charge", h.postChargeForecast)
		r.Post("/plan", h.postShiftPlan)
		r.Post("/work-units", h.postWorkUnit)
		r.Post("/release", h.postRelease)
		r.Get("/telemetry", h.getTelemetry)
		r.Get("/rebalance", h.getRebalance)
		r.Get("/labor-plan-view", h.getLaborPlanView)
	})

	r.Post("/work-units/{id}/complete", h.postComplete)

	r.Get("/inventory-view/{sku}", h.getInventoryView)

	return r
}
