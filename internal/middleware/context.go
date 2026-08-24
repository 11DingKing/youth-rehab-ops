package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/11DingKing/youth-rehab-ops/internal/domain"
)

type key int

const (
	requestIDKey key = iota
	userKey
	tokenKey
)

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func WithIdentity(ctx context.Context, user domain.User, token string) context.Context {
	ctx = context.WithValue(ctx, userKey, user)
	return context.WithValue(ctx, tokenKey, token)
}

func User(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(userKey).(domain.User)
	return user, ok
}

func Token(ctx context.Context) string {
	token, _ := ctx.Value(tokenKey).(string)
	return token
}

func Bearer(r *http.Request) string {
	scheme, token, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func NewRequestID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "request-unavailable"
	}
	return hex.EncodeToString(buffer)
}
