package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type correlationIDKey struct{}

const CorrelationIDHeader = "X-Correlation-ID"

func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(CorrelationIDHeader)
		if id == "" {
			id = newCorrelationID()
		}
		w.Header().Set(CorrelationIDHeader, id)
		ctx := context.WithValue(r.Context(), correlationIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CorrelationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(correlationIDKey{}).(string)
	return id
}

func newCorrelationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "request_unknown"
	}
	return "request_" + hex.EncodeToString(b[:])
}
