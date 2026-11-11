package api

import (
	"github.com/ben/meals/internal/db"
	"net/http"
	"time"
)

type ok map[string]any

func (a *API) done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ok{"ok": true})
}

// ---- plan ----

// getPlan answers for the day the browser says it is (?today=YYYY-MM-DD),
// which may differ from the server's date.
func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	today := r.URL.Query().Get("today")
	if today == "" {
		today = time.Now().Format(time.DateOnly)
	}
	plan, err := a.store.Plan(today)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

type slotRequest struct {
	RecipeID string `json:"recipe_id"`
	Text     string `json:"text"`
	Status   string `json:"status"`
}

func (a *API) setSlot(w http.ResponseWriter, r *http.Request) {
	var req slotRequest
	if !decode(w, r, &req) {
		return
	}
	date, meal := r.PathValue("date"), r.PathValue("meal")
	if req.RecipeID != "" {
		err := a.store.SetSlotRecipe(date, meal, req.RecipeID)
		if err == nil && req.Status != "" {
			err = a.store.SetSlotStatus(date, meal, req.Status)
		}
		a.done(w, r, err)
		return
	}
	a.done(w, r, a.store.SetSlotText(date, meal, req.Text))
}

func (a *API) clearSlot(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.ClearSlot(r.PathValue("date"), r.PathValue("meal")))
}

func (a *API) copyDay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.done(w, r, a.store.CopyDay(req.From, req.To))
}

func (a *API) validatePlan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Today string `json:"today"`
	}
	if !decode(w, r, &req) {
		return
	}
	n, err := a.store.Validate(req.Today)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ok{"validated": n})
}

// ---- recipes ----

func (a *API) listRecipes(w http.ResponseWriter, r *http.Request) {
	recipes, err := a.store.ListRecipes()
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, recipes)
}

func (a *API) createRecipe(w http.ResponseWriter, r *http.Request) {
	var in db.RecipeInput
	if !decode(w, r, &in) {
		return
	}
	recipe, err := a.store.CreateRecipe(in)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, recipe)
}

func (a *API) updateRecipe(w http.ResponseWriter, r *http.Request) {
	var in db.RecipeInput
	if !decode(w, r, &in) {
		return
	}
	recipe, err := a.store.UpdateRecipe(r.PathValue("id"), in)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, recipe)
}

func (a *API) deleteRecipe(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.DeleteRecipe(r.PathValue("id")))
}

// ---- shopping ----

func (a *API) getShopping(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.Shopping()
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (a *API) setChecked(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Checked bool   `json:"checked"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.done(w, r, a.store.SetChecked(req.Name, req.Checked))
}

func (a *API) addExtra(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.done(w, r, a.store.AddExtra("", req.Name))
}

func (a *API) deleteExtra(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.DeleteExtra(r.PathValue("id")))
}

func (a *API) setIngredientAisle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		AisleID string `json:"aisle_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.done(w, r, a.store.SetIngredientAisle(req.Name, req.AisleID))
}

func (a *API) finishShopping(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.FinishShopping())
}

// ---- aisles ----

func (a *API) listAisles(w http.ResponseWriter, r *http.Request) {
	aisles, err := a.store.ListAisles()
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, aisles)
}

type aisleRequest struct {
	Name string `json:"name"`
}

func (a *API) createAisle(w http.ResponseWriter, r *http.Request) {
	var req aisleRequest
	if !decode(w, r, &req) {
		return
	}
	aisle, err := a.store.CreateAisle("", req.Name)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, aisle)
}

func (a *API) renameAisle(w http.ResponseWriter, r *http.Request) {
	var req aisleRequest
	if !decode(w, r, &req) {
		return
	}
	a.done(w, r, a.store.RenameAisle(r.PathValue("id"), req.Name))
}

func (a *API) deleteAisle(w http.ResponseWriter, r *http.Request) {
	a.done(w, r, a.store.DeleteAisle(r.PathValue("id")))
}

func (a *API) reorderAisles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	a.done(w, r, a.store.ReorderAisles(req.IDs))
}
