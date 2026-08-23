package telemetry

import (
	"context"
	"log/slog"

	"github.com/go-logr/logr"
)

// slogSink adapts logr.LogSink to slog so the OTel SDK's own diagnostics
// (exporter retries, unparseable endpoints, dropped spans) come out as JSON
// on the same logger as everything else, instead of the SDK's default
// plain-text stderr writer. They are informational, not service errors, so
// they are emitted below Info: V(0) maps to Debug and higher verbosities
// stay there.
type slogSink struct {
	logger *slog.Logger
	name   string
	attrs  []any
}

// NewLogr returns a logr.Logger that writes through logger, suitable for
// otel.SetLogger.
func NewLogr(logger *slog.Logger) logr.Logger {
	return logr.New(&slogSink{logger: logger})
}

func (s *slogSink) Init(logr.RuntimeInfo) {}

func (s *slogSink) Enabled(int) bool {
	return s.logger.Enabled(context.Background(), slog.LevelDebug)
}

func (s *slogSink) Info(_ int, msg string, keysAndValues ...any) {
	s.logger.Debug(msg, s.args(keysAndValues)...)
}

func (s *slogSink) Error(err error, msg string, keysAndValues ...any) {
	s.logger.Debug(msg, append(s.args(keysAndValues), "error", err)...)
}

func (s *slogSink) WithValues(keysAndValues ...any) logr.LogSink {
	return &slogSink{logger: s.logger, name: s.name, attrs: s.args(keysAndValues)}
}

func (s *slogSink) WithName(name string) logr.LogSink {
	if s.name != "" {
		name = s.name + "/" + name
	}
	return &slogSink{logger: s.logger, name: name, attrs: s.attrs}
}

// args merges the sink's accumulated attributes, its logger name, and the
// call's own key/value pairs into one slog argument slice.
func (s *slogSink) args(keysAndValues []any) []any {
	out := make([]any, 0, len(s.attrs)+len(keysAndValues)+2)
	out = append(out, s.attrs...)
	out = append(out, keysAndValues...)
	if s.name != "" {
		out = append(out, "logger", s.name)
	}
	return out
}
