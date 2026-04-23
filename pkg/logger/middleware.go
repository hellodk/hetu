package logger

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// responseRecorder wraps http.ResponseWriter to capture the status code.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// RequestLogger is an HTTP middleware that:
//   - reads X-Request-ID from the incoming request (or generates a UUID v4)
//   - injects the ID into the request context via WithRequestID
//   - sets X-Request-ID on the response header
//   - logs method, path, status, and duration at Info level after the handler returns
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}

		ctx := WithRequestID(r.Context(), id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)

		rec := &responseRecorder{ResponseWriter: w, status: 0}
		start := time.Now()

		defer func() {
			statusToLog := rec.status
			if statusToLog == 0 {
				statusToLog = http.StatusOK
			}
			if p := recover(); p != nil {
				log.Ctx(ctx).Error().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Int("status", http.StatusInternalServerError).
					Int64("duration_ms", time.Since(start).Milliseconds()).
					Interface("panic", p).
					Msg("http request panic")
				panic(p) // re-panic so net/http can handle it
			}
			log.Ctx(ctx).Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", statusToLog).
				Int64("duration_ms", time.Since(start).Milliseconds()).
				Msg("http request")
		}()

		next.ServeHTTP(rec, r)
	})
}
