package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesIndexAtRoot(t *testing.T) {
	h := newTestHandler(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("expected HTML doctype in response")
	}
	if !strings.Contains(body, "Mini LLM Gateway") {
		t.Errorf("expected page title in body")
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

func TestServesStaticAssets(t *testing.T) {
	cases := []struct {
		path        string
		contentType string
		mustContain string
	}{
		{"/style.css", "text/css", ".container"},
		{"/app.js", "text/javascript", "consumeSSE"},
	}
	h := newTestHandler(t)
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, c.path, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, c.contentType) {
				t.Errorf("content-type = %q, want prefix %q", ct, c.contentType)
			}
			if !strings.Contains(rr.Body.String(), c.mustContain) {
				t.Errorf("expected response to contain %q", c.mustContain)
			}
		})
	}
}

func TestStaticAsset404(t *testing.T) {
	h := newTestHandler(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/does-not-exist.txt", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// API routes still take precedence over the catch-all file server.
func TestAPIRoutesNotShadowedByFileServer(t *testing.T) {
	h := newTestHandler(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Errorf("/health body = %q", rr.Body.String())
	}
}
