package access

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireRoleRejectsMissingRole(t *testing.T) {
	authenticator := roleTestAuthenticator{principal: Principal{Subject: "user-1", Roles: []string{"member"}}}
	handler := Require(authenticator, RequireRole("staff", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/staff", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
}

type roleTestAuthenticator struct {
	principal Principal
}

func (a roleTestAuthenticator) Authenticate(string, time.Time) (Principal, error) {
	return a.principal, nil
}
