// Package api exposes the store over a JSON HTTP API. Authentication and
// offline sync come from the kit.
package api

import (
	"fmt"
	"net/http"

	"github.com/bhr-tools/kit/auth"
	"github.com/bhr-tools/kit/httpx"
	"github.com/bhr-tools/kit/offline"
	"github.com/bhr-tools/meals/internal/db"
)

// sessionCookie keeps its first name: renaming it would sign everybody out.
const sessionCookie = "meals_session"

// API holds the HTTP handlers.
type API struct {
	store *db.Store
	auth  *auth.Auth
	sync  *offline.Sync
}

// New returns the API. With noAuth, every request is accepted without login.
func New(store *db.Store, noAuth bool) (*API, error) {
	a := &API{store: store}
	var err error
	if a.auth, err = auth.New(store.DB(), sessionCookie, noAuth); err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if a.sync, err = offline.New(store.DB(), a.snapshot, a.dispatch); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	return a, nil
}

// Helpers of the kit, under the names the handlers use.
var (
	writeJSON  = httpx.WriteJSON
	writeError = httpx.WriteError
	fail       = httpx.Fail
	decode     = httpx.Decode
)

// Handler returns the API routes, wrapped in their middlewares.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	open := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, h) }
	auth := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.auth.Require(h)) }

	a.auth.Routes(mux)
	a.sync.Routes(mux, a.auth.Require)

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

	auth("GET /api/aisles", a.listAisles)
	auth("POST /api/aisles", a.createAisle)
	auth("PATCH /api/aisles/{id}", a.renameAisle)
	auth("DELETE /api/aisles/{id}", a.deleteAisle)
	auth("PUT /api/aisles/order", a.reorderAisles)

	open("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Unknown API endpoint")
	})
	return httpx.RejectCrossSite(mux)
}
