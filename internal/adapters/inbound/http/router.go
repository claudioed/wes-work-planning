package http

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/riandyrn/otelchi"
	otelchimetric "github.com/riandyrn/otelchi/metric"
)

// NewRouter wires every REST endpoint to its handler.
//
// serviceName names the server in the OTel span and metric attributes;
// logger, when non-nil, enables the structured per-request access log. Each
// request gets a server span named after its chi route pattern (not the raw
// path, which would blow up span cardinality on the {pathId}/{id}/{sku}
// segments) plus the semconv http.server.request.duration histogram.
func NewRouter(h *Handlers, serviceName string, logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()

	metricCfg := otelchimetric.NewBaseConfig(serviceName)

	r.Use(middleware.RequestID)
	r.Use(otelchi.Middleware(serviceName, otelchi.WithChiRoutes(r)))
	r.Use(otelchimetric.NewServerRequestDuration(metricCfg))
	r.Use(otelchimetric.NewServerActiveRequests(metricCfg))
	if logger != nil {
		r.Use(RequestLogger(logger))
	}
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware())

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

// corsMiddleware allows the warehouse-console browser SPA (and this
// service's own future MFE remote dev origin) to call this API directly
// from the browser. Static-bearer-key auth, not cookies, so credentials
// are never needed here. CORS_ALLOWED_ORIGINS overrides the local-dev
// default (comma-separated) for staging/prod deployments.
func corsMiddleware() func(http.Handler) http.Handler {
	origins := []string{"http://localhost:5173", "http://localhost:5183"}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		origins = strings.Split(v, ",")
	}
	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           300,
	})
}
