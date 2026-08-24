package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

type Authenticator interface {
	Authenticate(ctx interface{ Done() <-chan struct{} }, token string) (domain.User, error)
}

type UserAuthenticator func(*http.Request, string) (domain.User, error)

func RequestContext(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" || len(requestID) > 128 {
			requestID = NewRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		ctx := WithRequestID(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
		logger.InfoContext(ctx, "http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds(), "request_id", requestID)
	})
}

func Recover(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(r.Context(), "panic recovered", "panic", recovered, "stack", string(debug.Stack()), "request_id", RequestID(r.Context()))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"internal server error"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func Authenticate(auth UserAuthenticator, unauthorized func(http.ResponseWriter, *http.Request, error), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := Bearer(r)
		user, err := auth(r, token)
		if err != nil {
			unauthorized(w, r, err)
			return
		}
		ctx := WithIdentity(r.Context(), user, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
