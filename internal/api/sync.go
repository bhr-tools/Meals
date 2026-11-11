package api

// Offline sync (see the offline plan): GET /api/sync returns all the data;
// POST /api/sync applies changes made offline, in order, then returns the new
// data. Each change has a client-chosen UUID, recorded once applied, so a
// batch sent twice (lost answer, retry) is applied once.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/ben/meals/internal/db"
)

const maxSyncOps = 500

type syncOp struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Args json.RawMessage `json:"args"`
}

type syncResult struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok | not_found | invalid
	Error  string `json:"error,omitempty"`
}

type syncResponse struct {
	Results  []syncResult    `json:"results,omitempty"`
	ETag     string          `json:"etag"`
	Snapshot json.RawMessage `json:"snapshot"`
}

// snapshot returns the encoded snapshot and its ETag.
func (a *API) snapshot() (json.RawMessage, string, error) {
	snap, err := a.store.Snapshot()
	if err != nil {
		return nil, "", err
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(body)
	return body, `"` + hex.EncodeToString(sum[:16]) + `"`, nil
}

func (a *API) getSync(w http.ResponseWriter, r *http.Request) {
	body, etag, err := a.snapshot()
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, syncResponse{ETag: etag, Snapshot: body})
}

func (a *API) postSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ops []syncOp `json:"ops"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Ops) > maxSyncOps {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Too many changes at once (max %d)", maxSyncOps))
		return
	}
	results := make([]syncResult, 0, len(req.Ops))
	for _, op := range req.Ops {
		res, err := a.applySyncOp(op)
		if err != nil {
			fail(w, r, err) // the client sends the batch again; applied changes are skipped
			return
		}
		results = append(results, res)
	}
	if err := a.store.PruneSyncOps(); err != nil {
		log.Printf("prune sync ops: %v", err)
	}
	body, etag, err := a.snapshot()
	if err != nil {
		fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, syncResponse{Results: results, ETag: etag, Snapshot: body})
}

// applySyncOp applies one change once. Only internal errors are returned.
func (a *API) applySyncOp(op syncOp) (syncResult, error) {
	res := syncResult{ID: op.ID, Status: "ok"}
	if _, err := uuid.Parse(op.ID); err != nil {
		res.Status, res.Error = "invalid", "Invalid operation id"
		return res, nil
	}
	status, msg, found, err := a.store.SyncOpStatus(op.ID)
	if err != nil {
		return res, err
	}
	if found {
		res.Status, res.Error = status, msg
		return res, nil
	}
	var invalidErr *db.ValidationError
	switch err := a.dispatch(op); {
	case err == nil:
	case errors.Is(err, db.ErrNotFound):
		res.Status = "not_found"
	case errors.As(err, &invalidErr):
		res.Status, res.Error = "invalid", invalidErr.Msg
	default:
		return res, err
	}
	return res, a.store.RecordSyncOp(op.ID, res.Status, res.Error)
}

// args decodes the arguments of a change; a malformed change is invalid.
func args(op syncOp, dst any) error {
	if err := json.Unmarshal(op.Args, dst); err != nil {
		return &db.ValidationError{Msg: "Invalid arguments: " + err.Error()}
	}
	return nil
}

// dispatch applies one change with the matching store method.
func (a *API) dispatch(op syncOp) error {
	s := a.store
	switch op.Type {
	case "slot.set_recipe":
		var x struct {
			Date     string `json:"date"`
			Meal     string `json:"meal"`
			RecipeID string `json:"recipe_id"`
			Status   string `json:"status"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		err := s.SetSlotRecipe(x.Date, x.Meal, x.RecipeID)
		if err == nil && x.Status != "" {
			err = s.SetSlotStatus(x.Date, x.Meal, x.Status)
		}
		return err
	case "slot.set_text":
		var x struct {
			Date string `json:"date"`
			Meal string `json:"meal"`
			Text string `json:"text"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.SetSlotText(x.Date, x.Meal, x.Text)
	case "slot.clear":
		var x struct {
			Date string `json:"date"`
			Meal string `json:"meal"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.ClearSlot(x.Date, x.Meal)
	case "plan.copy_day":
		var x struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.CopyDay(x.From, x.To)
	case "plan.validate":
		var x struct {
			Today string `json:"today"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		_, err := s.Validate(x.Today)
		return err
	case "recipe.create":
		var in db.RecipeInput
		if err := args(op, &in); err != nil {
			return err
		}
		if in.ID != "" {
			if _, err := s.GetRecipe(in.ID); err == nil {
				return nil
			}
		}
		_, err := s.CreateRecipe(in)
		return err
	case "recipe.update":
		var in db.RecipeInput
		if err := args(op, &in); err != nil {
			return err
		}
		_, err := s.UpdateRecipe(in.ID, in)
		return err
	case "recipe.delete":
		var x struct {
			ID string `json:"id"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.DeleteRecipe(x.ID)
	case "shopping.check":
		var x struct {
			Name    string `json:"name"`
			Checked bool   `json:"checked"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.SetChecked(x.Name, x.Checked)
	case "shopping.add_extra":
		var x struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.AddExtra(x.ID, x.Name)
	case "shopping.remove_extra":
		var x struct {
			ID string `json:"id"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.DeleteExtra(x.ID)
	case "shopping.set_aisle":
		var x struct {
			Name    string `json:"name"`
			AisleID string `json:"aisle_id"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.SetIngredientAisle(x.Name, x.AisleID)
	case "shopping.done":
		return s.FinishShopping()
	case "aisle.create":
		var x struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		if x.ID != "" {
			if found, err := s.HasAisle(x.ID); err != nil || found {
				return err
			}
		}
		_, err := s.CreateAisle(x.ID, x.Name)
		return err
	case "aisle.rename":
		var x struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.RenameAisle(x.ID, x.Name)
	case "aisle.delete":
		var x struct {
			ID string `json:"id"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.DeleteAisle(x.ID)
	case "aisle.reorder":
		var x struct {
			IDs []string `json:"ids"`
		}
		if err := args(op, &x); err != nil {
			return err
		}
		return s.ReorderAisles(x.IDs)
	}
	return &db.ValidationError{Msg: fmt.Sprintf("Unknown operation %q", op.Type)}
}
