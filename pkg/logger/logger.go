package logger

import (
	"context"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type contextKey struct{}

// Init configures the global zerolog logger.
// level: "debug"|"info"|"warn"|"error" — defaults to "info" on unknown values.
// format: "json"|"pretty" — "pretty" uses ConsoleWriter for local dev.
func Init(level, format string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	lvl, err := zerolog.ParseLevel(level)
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	if format == "pretty" {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	} else {
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
}

// WithRequestID stores a request ID in the context for downstream use.
// It also attaches the ID to the zerolog context so log.Ctx(ctx) carries it.
func WithRequestID(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, contextKey{}, id)
	l := log.Ctx(ctx).With().Str("request_id", id).Logger()
	return l.WithContext(ctx)
}

// RequestIDFromContext retrieves the request ID stored by WithRequestID.
// Returns "" if not set.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(contextKey{}).(string); ok {
		return id
	}
	return ""
}
