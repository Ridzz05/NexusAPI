package access

import (
	"context"
	"net/http"
	"time"

	"github.com/Ridzz05/NexusAPI/internal/platform/httpx"
)

type principalKey struct{}

func Require(authenticator Authenticator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authenticator == nil {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "authentication is required")
			return
		}
		token, err := BearerToken(r.Header.Get("Authorization"))
		if err != nil {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "authentication is required")
			return
		}
		principal, err := authenticator.Authenticate(token, time.Now())
		if err != nil {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "authentication is required")
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

func RequireRole(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok || !HasRole(principal, role) {
			httpx.Error(w, r, http.StatusForbidden, "forbidden", "you do not have permission to access this resource")
			return
		}
		next.ServeHTTP(w, r)
	})
}
