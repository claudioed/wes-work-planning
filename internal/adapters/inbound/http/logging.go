package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RequestLogger returns chi middleware that logs each request at Info level
// (Error for 5xx) with method, route pattern, status, duration, response
// size, and request ID. It logs through the request context, so the
// telemetry slog handler can attach the trace_id/span_id of the span
// otelchi opened for this request.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			next.ServeHTTP(ww, r)

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"route", routePattern(r),
				"status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", ww.BytesWritten(),
				"request_id", middleware.GetReqID(r.Context()),
			}

			if ww.Status() >= http.StatusInternalServerError {
				logger.ErrorContext(r.Context(), "http request", attrs...)
			} else {
				logger.InfoContext(r.Context(), "http request", attrs...)
			}
		})
	}
}

// routePattern returns the matched chi route template (e.g.
// /paths/{pathId}/release) rather than the raw path, keeping the log field
// low-cardinality and aligned with the span name otelchi produces.
func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return r.URL.Path
}
