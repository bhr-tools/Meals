package api

// Offline sync: the kit (kit/offline) serves /api/sync and applies each change
// once; this file gives it Meals' snapshot and how to apply one change.

import (
	"fmt"

	"github.com/bhr-tools/kit/offline"
	"github.com/bhr-tools/meals/internal/db"
)

// snapshot is all the data.
func (a *API) snapshot() (any, error) {
	return a.store.Snapshot()
}

// args decodes the arguments of a change; a malformed change is invalid.
func args(op offline.Op, dst any) error {
	return op.Decode(dst)
}

// dispatch applies one change with the matching store method.
func (a *API) dispatch(op offline.Op) error {
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
