package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bhr-tools/meals/internal/api"
	"github.com/bhr-tools/meals/internal/db"
)

func handler(t *testing.T, store *db.Store) http.Handler {
	t.Helper()
	a, err := api.New(store, true)
	if err != nil {
		t.Fatal(err)
	}
	return newHandler(store, a)
}

func TestServiceWorker(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "sw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := handler(t, store)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/sw.js", nil))
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("GET /sw.js = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.HasPrefix(body, `const VERSION = "`) {
		t.Errorf("body starts with %.40q", body)
	}
	for _, want := range []string{`"/"`, `"/js/app.js"`, `"/kit/js/overlay.js"`, "self.addEventListener"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s", want)
		}
	}
	if strings.Contains(body, `"/index.html"`) {
		t.Error("index.html must be listed as /")
	}

	// The app itself is still served.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Meals</title>") {
		t.Errorf("GET / = %d", rec.Code)
	}
}
