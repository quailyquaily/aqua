package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWebConsoleStaticDir(t *testing.T) {
	t.Parallel()

	t.Run("rejects empty", func(t *testing.T) {
		t.Parallel()

		got, err := validateWebConsoleStaticDir("")
		if err != nil {
			t.Fatalf("validateWebConsoleStaticDir() error = %v", err)
		}
		if got != "" {
			t.Fatalf("validated dir mismatch: got %q want empty", got)
		}
	})

	t.Run("rejects missing index", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if _, err := validateWebConsoleStaticDir(dir); err == nil {
			t.Fatalf("expected error for missing index.html")
		}
	})

	t.Run("accepts directory with index", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>ok</html>"), 0o600); err != nil {
			t.Fatalf("write index: %v", err)
		}
		got, err := validateWebConsoleStaticDir(dir)
		if err != nil {
			t.Fatalf("validateWebConsoleStaticDir() error = %v", err)
		}
		if got != dir {
			t.Fatalf("validated dir mismatch: got %q want %q", got, dir)
		}
	})
}

func TestWebConsoleStaticHandler_SPAFallback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>index</html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('asset')"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	handler := newWebConsoleStaticHandler(dir)

	t.Run("serves asset file", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "console.log") {
			t.Fatalf("expected asset contents, got %q", rec.Body.String())
		}
	})

	t.Run("falls back to index for spa route", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "<html>index</html>") {
			t.Fatalf("expected index fallback, got %q", rec.Body.String())
		}
	})
}

func TestWebConsoleHTTPHandler_APIOnlyMode(t *testing.T) {
	t.Parallel()

	handler := newWebConsoleHTTPHandler(&webConsoleServer{
		cfg: webConsoleConfig{},
	})

	t.Run("returns 404 for root without static dir", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("serves health check without static dir", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/api/console/v1/_hc", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), `"status": "ok"`) {
			t.Fatalf("expected health payload, got %q", rec.Body.String())
		}
	})

	t.Run("answers api cors preflight", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodOptions, "/api/console/v1/contacts", nil)
		req.Header.Set("Origin", "http://127.0.0.1:3000")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3000" {
			t.Fatalf("allow origin = %q, want %q", got, "http://127.0.0.1:3000")
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
			t.Fatalf("allow headers = %q, want %q", got, "Content-Type")
		}
	})
}
