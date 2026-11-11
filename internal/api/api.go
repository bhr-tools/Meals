// Package api exposes the store over a JSON HTTP API.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/ben/meals/internal/db"
)

const (
	maxJSONBody   = 1 << 20 // 1 MiB
	sessionCookie = "meals_session"
)

// API holds the HTTP handlers.
type API struct {
	store  *db.Store
	noAuth bool
	// loginMu serializes password checks; a failed attempt holds it for a
	// moment, which caps brute-force attempts without any bookkeeping.
	loginMu sync.Mutex
}

// New returns the API. With noAuth, every request is accepted without login.
func New(store *db.Store, noAuth bool) *API {
	return &API{store: store, noAuth: noAuth}
}

// Handler returns the API routes, wrapped in their middlewares.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	open := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, h) }
	auth := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.requireAuth(h)) }

	open("GET /api/auth/status", a.authStatus)
	open("POST /api/auth/setup", a.authSetup)
	open("POST /api/auth/login", a.authLogin)
	open("POST /api/auth/logout", a.authLogout)
	auth("POST /api/auth/change-password", a.authChangePassword)

	auth("GET /api/plan", a.getPlan)
	auth("PUT /api/slots/{date}/{meal}", a.setSlot)
	auth("DELETE /api/slots/{date}/{meal}", a.clearSlot)
	auth("POST /api/plan/copy-day", a.copyDay)
	auth("POST /api/plan/validate", a.validatePlan)

	auth("GET /api/recipes", a.listRecipes)
	auth("POST /api/recipes", a.createRecipe)
	auth("PUT /api/recipes/{id}", a.updateRecipe)
	auth("DELETE /api/recipes/{id}", a.deleteRecipe)

	auth("GET /api/shopping", a.getShopping)
	auth("PUT /api/shopping/checked", a.setChecked)
	auth("POST /api/shopping/extra", a.addExtra)
	auth("DELETE /api/shopping/extra/{id}", a.deleteExtra)
	auth("PUT /api/shopping/aisle", a.setIngredientAisle)
	auth("POST /api/shopping/done", a.finishShopping)

	auth("GET /api/sync", a.getSync)
	auth("POST /api/sync", a.postSync)

	auth("GET /api/aisles", a.listAisles)
	auth("POST /api/aisles", a.createAisle)
	auth("PATCH /api/aisles/{id}", a.renameAisle)
	auth("DELETE /api/aisles/{id}", a.deleteAisle)
	auth("PUT /api/aisles/order", a.reorderAisles)

	open("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Unknown API endpoint")
	})
	return rejectCrossSite(mux)
}

// rejectCrossSite refuses state-changing requests sent by another site
// (CSRF). Browsers label every request with Sec-Fetch-Site; requests without
// it (curl, scripts) are not browser-driven and are let through.
func rejectCrossSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusForbidden, "Cross-site request refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Authentication required", "auth_required": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticated reports whether r may use the API. Until a password is set,
// the instance is open (first-run setup).
func (a *API) authenticated(r *http.Request) bool {
	if a.noAuth {
		return true
	}
	hasPassword, err := a.store.HasPassword()
	if err != nil {
		return false
	}
	if !hasPassword {
		return true
	}
	c, err := r.Cookie(sessionCookie)
	return err == nil && a.store.ValidateSession(c.Value)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail answers with the status matching err: 404, 400 for invalid input, or
// 500 (logged, with a generic message).
func fail(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *db.ValidationError
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, invalid.Msg)
	default:
		log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
	}
}

// decode reads a JSON request body into dst. On failure it answers the
// request and returns false.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	body := http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(body).Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "Request body too large")
		case errors.Is(err, io.EOF):
			writeError(w, http.StatusBadRequest, "Request body is empty")
		default:
			writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		}
		return false
	}
	return true
}
