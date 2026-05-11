package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

func TestRequireBearerNoTokenIsPassthrough(t *testing.T) {
	h := RequireBearer("")(okHandler())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth should be disabled when token is empty)", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rr.Body.String())
	}
}

func TestRequireBearerMissingHeaderRejects(t *testing.T) {
	h := RequireBearer("secret")(okHandler())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unauthorized") {
		t.Errorf("body should mention unauthorized: %s", rr.Body.String())
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("expected WWW-Authenticate header on 401")
	}
}

func TestRequireBearerWrongTokenRejects(t *testing.T) {
	h := RequireBearer("right")(okHandler())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestRequireBearerNonBearerSchemeRejects(t *testing.T) {
	h := RequireBearer("secret")(okHandler())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Basic c2VjcmV0OnNlY3JldA==")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestRequireBearerCorrectTokenPasses(t *testing.T) {
	h := RequireBearer("right-token")(okHandler())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer right-token")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

// Length-mismatched tokens must still be rejected (and not panic from
// the constant-time compare).
func TestRequireBearerDifferentLengthRejects(t *testing.T) {
	h := RequireBearer("longer-token-here")(okHandler())
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	req.Header.Set("Authorization", "Bearer x")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}
