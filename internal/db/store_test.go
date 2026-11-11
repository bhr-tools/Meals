package db

import (
	"errors"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "meals.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustRecipe(t *testing.T, s *Store, title string, ingredients ...string) Recipe {
	t.Helper()
	r, err := s.CreateRecipe(RecipeInput{Title: title, Ingredients: ingredients})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func isInvalid(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

const today = "2026-11-02"

func TestRecipeCleansIngredients(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "  Gratin ", "Courgettes", "  ", "courgette", "Crème  fraîche")
	if r.Title != "Gratin" {
		t.Errorf("title = %q", r.Title)
	}
	if len(r.Ingredients) != 2 || r.Ingredients[0] != "Courgettes" || r.Ingredients[1] != "Crème fraîche" {
		t.Errorf("ingredients = %q", r.Ingredients)
	}
	if _, err := s.CreateRecipe(RecipeInput{Title: " "}); !isInvalid(err) {
		t.Errorf("empty title: err = %v", err)
	}
	if _, err := s.UpdateRecipe("nope", RecipeInput{Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown recipe: err = %v", err)
	}
}

func TestNotesStayOutOfTheShoppingList(t *testing.T) {
	s := newStore(t)
	r, err := s.CreateRecipe(RecipeInput{Title: "Soup", Ingredients: []string{"leek"}, Notes: "simmer 20 min\nadd salt"})
	must(t, err)
	must(t, s.SetSlotRecipe(today, "dinner", r.ID))
	_, err = s.Validate(today)
	must(t, err)
	list, err := s.Shopping()
	must(t, err)
	if list.Total != 1 || list.Groups[0].Items[0].Name != "leek" {
		t.Errorf("list = %+v", list)
	}
}

func TestPlanWindowAndPending(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "Pasta", "pasta")
	must(t, s.SetSlotRecipe("2026-11-02", "lunch", r.ID))
	must(t, s.SetSlotText("2026-11-08", "dinner", "  out  "))
	must(t, s.SetSlotRecipe("2026-11-09", "lunch", r.ID)) // outside the window
	must(t, s.SetSlotRecipe("2026-11-01", "lunch", r.ID)) // yesterday

	p, err := s.Plan(today)
	must(t, err)
	if len(p.Days) != 7 || p.Days[0].Date != "2026-11-02" || p.Days[6].Date != "2026-11-08" {
		t.Fatalf("days = %+v", p.Days)
	}
	if p.Days[0].Lunch == nil || p.Days[0].Lunch.Title != "Pasta" || p.Days[0].Lunch.Status != StatusPending {
		t.Errorf("monday lunch = %+v", p.Days[0].Lunch)
	}
	if s := p.Days[6].Dinner; s == nil || s.Text != "out" || s.RecipeID != nil || s.Status != StatusDone {
		t.Errorf("sunday dinner = %+v", s)
	}
	if p.Pending != 1 {
		t.Errorf("pending = %d, want 1", p.Pending)
	}
	if _, err := s.Plan("garbage"); !isInvalid(err) {
		t.Errorf("bad date: err = %v", err)
	}
	if err := s.SetSlotText(today, "brunch", "x"); !isInvalid(err) {
		t.Errorf("bad meal: err = %v", err)
	}
}

func TestOnlyNewRecipesJoinTheList(t *testing.T) {
	s := newStore(t)
	bolo := mustRecipe(t, s, "Bolognese", "pasta", "onion", "ground beef")
	chili := mustRecipe(t, s, "Chili", "rice", "onion")

	// First batch: two bolognese.
	must(t, s.SetSlotRecipe("2026-11-02", "dinner", bolo.ID))
	must(t, s.SetSlotRecipe("2026-11-03", "dinner", bolo.ID))
	n, err := s.Validate(today)
	must(t, err)
	if n != 2 {
		t.Fatalf("validated %d, want 2", n)
	}
	list, _ := s.Shopping()
	if list.Total != 3 {
		t.Fatalf("first list has %d items, want 3", list.Total)
	}

	// Shopping done: the list empties, the planned meals stay.
	must(t, s.FinishShopping())
	list, _ = s.Shopping()
	if list.Total != 0 {
		t.Fatalf("list after done has %d items", list.Total)
	}
	p, _ := s.Plan(today)
	if p.Days[0].Dinner.Status != StatusDone {
		t.Errorf("status = %q, want done", p.Days[0].Dinner.Status)
	}

	// Second batch: only the new recipes appear.
	must(t, s.SetSlotRecipe("2026-11-04", "lunch", chili.ID))
	must(t, s.SetSlotRecipe("2026-11-05", "lunch", chili.ID))
	p, _ = s.Plan(today)
	if p.Pending != 2 {
		t.Errorf("pending = %d, want 2", p.Pending)
	}
	_, err = s.Validate(today)
	must(t, err)
	list, _ = s.Shopping()
	if list.Total != 2 {
		t.Fatalf("second list has %d items, want 2: %+v", list.Total, list)
	}
}

func TestIngredientListsItsRecipes(t *testing.T) {
	s := newStore(t)
	bolo := mustRecipe(t, s, "Bolognese", "pasta", "onion")
	chili := mustRecipe(t, s, "Chili", "rice", "Onions")
	for _, d := range []string{"2026-11-02", "2026-11-03"} {
		must(t, s.SetSlotRecipe(d, "dinner", bolo.ID))
	}
	for _, d := range []string{"2026-11-04", "2026-11-05"} {
		must(t, s.SetSlotRecipe(d, "dinner", chili.ID))
	}
	must(t, s.SetSlotRecipe("2026-11-06", "dinner", chili.ID)) // chili ×3
	_, err := s.Validate(today)
	must(t, err)

	list, err := s.Shopping()
	must(t, err)
	var onion *ShoppingItem
	for _, g := range list.Groups {
		for i := range g.Items {
			if g.Items[i].Key == "onion" {
				onion = &g.Items[i]
			}
		}
	}
	if onion == nil || len(onion.Recipes) != 2 {
		t.Fatalf("onion = %+v", onion)
	}
	if r := onion.Recipes[0]; r.Title != "Bolognese" || r.Count != 2 {
		t.Errorf("first ref = %+v", r)
	}
	if r := onion.Recipes[1]; r.Title != "Chili" || r.Count != 3 {
		t.Errorf("second ref = %+v", r)
	}
}

func TestPendingStaysOutOfTheList(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "Soup", "leek")
	must(t, s.SetSlotRecipe(today, "lunch", r.ID))
	list, _ := s.Shopping()
	if list.Total != 0 {
		t.Errorf("pending recipe reached the list: %+v", list)
	}
}

func TestReplacingOrClearingAListedSlot(t *testing.T) {
	s := newStore(t)
	a := mustRecipe(t, s, "A", "apple")
	b := mustRecipe(t, s, "B", "bread")
	must(t, s.SetSlotRecipe(today, "lunch", a.ID))
	_, err := s.Validate(today)
	must(t, err)

	// Choosing the same recipe again keeps it listed.
	must(t, s.SetSlotRecipe(today, "lunch", a.ID))
	if list, _ := s.Shopping(); list.Total != 1 {
		t.Errorf("same recipe: total = %d, want 1", list.Total)
	}
	// A different recipe starts pending, and the old one leaves the list.
	must(t, s.SetSlotRecipe(today, "lunch", b.ID))
	p, _ := s.Plan(today)
	if p.Days[0].Lunch.Status != StatusPending {
		t.Errorf("status = %q, want pending", p.Days[0].Lunch.Status)
	}
	if list, _ := s.Shopping(); list.Total != 0 {
		t.Errorf("replaced recipe still listed: %+v", list)
	}
	// Clearing a listed slot removes its ingredients too.
	must(t, s.SetSlotRecipe(today, "dinner", a.ID))
	_, err = s.Validate(today)
	must(t, err)
	must(t, s.ClearSlot(today, "dinner"))
	// Only B's bread is left (the second Validate listed the lunch too).
	if list, _ := s.Shopping(); list.Total != 1 || list.Groups[0].Items[0].Key != "bread" {
		t.Errorf("cleared slot still listed: %+v", list)
	}
	must(t, s.ClearSlot(today, "dinner")) // clearing twice is fine
}

func TestTicksSurviveAndResetOnDone(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "Soup", "leek", "potato")
	must(t, s.SetSlotRecipe(today, "lunch", r.ID))
	_, err := s.Validate(today)
	must(t, err)
	must(t, s.SetChecked("Leeks", true))
	list, _ := s.Shopping()
	if list.Remaining != 1 || list.Total != 2 {
		t.Errorf("remaining = %d total = %d", list.Remaining, list.Total)
	}

	// A second batch keeps the tick on the shared ingredient.
	r2 := mustRecipe(t, s, "Quiche", "leek", "egg")
	must(t, s.SetSlotRecipe("2026-11-03", "lunch", r2.ID))
	_, err = s.Validate(today)
	must(t, err)
	list, _ = s.Shopping()
	if list.Total != 3 || list.Remaining != 2 {
		t.Errorf("after second batch: total = %d remaining = %d", list.Total, list.Remaining)
	}

	must(t, s.AddExtra("", "Toilet paper"))
	must(t, s.FinishShopping())
	list, _ = s.Shopping()
	if list.Total != 0 {
		t.Errorf("list not empty after done: %+v", list)
	}
	r3 := mustRecipe(t, s, "Leek pie", "leek")
	must(t, s.SetSlotRecipe("2026-11-04", "lunch", r3.ID))
	_, err = s.Validate(today)
	must(t, err)
	list, _ = s.Shopping()
	if list.Remaining != 1 {
		t.Errorf("tick leaked into the next list: %+v", list)
	}
}

func TestAislesGroupingAndOverrides(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "Mix", "courgette", "poulet", "zorglub")
	must(t, s.SetSlotRecipe(today, "lunch", r.ID))
	_, err := s.Validate(today)
	must(t, err)
	must(t, s.AddExtra("", "lait"))

	groupOf := func(key string) string {
		list, err := s.Shopping()
		must(t, err)
		for _, g := range list.Groups {
			for _, it := range g.Items {
				if it.Key == key {
					return g.Aisle
				}
			}
		}
		return ""
	}
	if got := groupOf("courgette"); got != "Produce" {
		t.Errorf("courgette in %q", got)
	}
	if got := groupOf("poulet"); got != "Meat & fish" {
		t.Errorf("poulet in %q", got)
	}
	if got := groupOf("zorglub"); got != "Other" {
		t.Errorf("zorglub in %q", got)
	}
	if got := groupOf("lait"); got != "Dairy & eggs" {
		t.Errorf("hand-written lait in %q", got)
	}

	// Aisles come out in store order.
	list, _ := s.Shopping()
	if list.Groups[0].Aisle != "Produce" || list.Groups[len(list.Groups)-1].Aisle != "Other" {
		t.Errorf("group order: %+v", list.Groups)
	}

	// A user choice beats the dictionary and is remembered.
	custom, err := s.CreateAisle("", "Butcher")
	must(t, err)
	must(t, s.SetIngredientAisle("Poulet", custom.ID))
	if got := groupOf("poulet"); got != "Butcher" {
		t.Errorf("after override poulet in %q", got)
	}
	// Custom aisles sit before Other.
	aisles, _ := s.ListAisles()
	if aisles[len(aisles)-1].Key != OtherAisleKey || aisles[len(aisles)-2].ID != custom.ID {
		t.Errorf("aisles = %+v", aisles)
	}
	// Deleting the aisle sends the ingredient back to the dictionary.
	must(t, s.DeleteAisle(custom.ID))
	if got := groupOf("poulet"); got != "Meat & fish" {
		t.Errorf("after delete poulet in %q", got)
	}
}

func TestAisleEditing(t *testing.T) {
	s := newStore(t)
	aisles, _ := s.ListAisles()
	if len(aisles) != 9 {
		t.Fatalf("%d default aisles", len(aisles))
	}
	var other, produce Aisle
	for _, a := range aisles {
		switch a.Key {
		case "other":
			other = a
		case "produce":
			produce = a
		}
	}
	if err := s.DeleteAisle(other.ID); !isInvalid(err) {
		t.Errorf("deleting Other: err = %v", err)
	}
	must(t, s.RenameAisle(produce.ID, "Fruit & veg"))
	if err := s.RenameAisle(produce.ID, " "); !isInvalid(err) {
		t.Errorf("empty name: err = %v", err)
	}
	// A renamed or reordered aisle still receives its dictionary ingredients.
	ids := []string{}
	for i := len(aisles) - 1; i >= 0; i-- {
		ids = append(ids, aisles[i].ID)
	}
	must(t, s.ReorderAisles(ids))
	if err := s.ReorderAisles(ids[1:]); !isInvalid(err) {
		t.Errorf("incomplete order: err = %v", err)
	}
	reordered, _ := s.ListAisles()
	if reordered[0].ID != aisles[len(aisles)-1].ID {
		t.Errorf("order = %+v", reordered)
	}
	r := mustRecipe(t, s, "Salad", "courgette")
	must(t, s.SetSlotRecipe(today, "lunch", r.ID))
	_, err := s.Validate(today)
	must(t, err)
	list, _ := s.Shopping()
	if list.Groups[0].Aisle != "Fruit & veg" {
		t.Errorf("group = %q", list.Groups[0].Aisle)
	}
}

func TestLastPlannedOrdersRecipes(t *testing.T) {
	s := newStore(t)
	old := mustRecipe(t, s, "Old", "a")
	recent := mustRecipe(t, s, "Recent", "b")
	never := mustRecipe(t, s, "Never", "c")
	must(t, s.SetSlotRecipe("2026-11-02", "lunch", old.ID))
	must(t, s.SetSlotRecipe("2026-11-05", "lunch", recent.ID))
	_, err := s.Validate(today)
	must(t, err)
	recipes, _ := s.ListRecipes()
	got := []string{recipes[0].Title, recipes[1].Title, recipes[2].Title}
	want := []string{never.Title, old.Title, recent.Title}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestCopyDay(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "Pasta", "pasta")
	must(t, s.SetSlotRecipe("2026-11-02", "lunch", r.ID))
	must(t, s.SetSlotText("2026-11-02", "dinner", "out"))
	_, err := s.Validate(today)
	must(t, err)
	must(t, s.SetSlotText("2026-11-03", "lunch", "will be replaced"))
	must(t, s.CopyDay("2026-11-02", "2026-11-03"))
	p, _ := s.Plan(today)
	if l := p.Days[1].Lunch; l == nil || l.Title != "Pasta" || l.Status != StatusPending {
		t.Errorf("copied lunch = %+v", l)
	}
	if d := p.Days[1].Dinner; d == nil || d.Text != "out" {
		t.Errorf("copied dinner = %+v", d)
	}
	if p.Days[0].Lunch.Status != StatusListed {
		t.Errorf("source changed: %+v", p.Days[0].Lunch)
	}
}

func TestDeletingARecipeClearsItsSlots(t *testing.T) {
	s := newStore(t)
	r := mustRecipe(t, s, "Pasta", "pasta")
	must(t, s.SetSlotRecipe(today, "lunch", r.ID))
	must(t, s.DeleteRecipe(r.ID))
	p, _ := s.Plan(today)
	if p.Days[0].Lunch != nil {
		t.Errorf("slot survived: %+v", p.Days[0].Lunch)
	}
	if err := s.DeleteRecipe(r.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete: err = %v", err)
	}
}

func TestExtraItems(t *testing.T) {
	s := newStore(t)
	must(t, s.AddExtra("", "Toilet paper"))
	must(t, s.AddExtra("", "toilet papers")) // same item
	list, _ := s.Shopping()
	if list.Total != 1 || list.Groups[0].Items[0].ExtraID == "" {
		t.Fatalf("list = %+v", list)
	}
	must(t, s.DeleteExtra(list.Groups[0].Items[0].ExtraID))
	if list, _ = s.Shopping(); list.Total != 0 {
		t.Errorf("extra not deleted: %+v", list)
	}
}

func TestRemovingAMealUpdatesLastPlanned(t *testing.T) {
	s := newStore(t)
	a := mustRecipe(t, s, "A", "apple")
	must(t, s.SetSlotRecipe(today, "lunch", a.ID))
	_, err := s.Validate(today)
	must(t, err)
	if rs, _ := s.ListRecipes(); rs[0].LastPlanned == nil || *rs[0].LastPlanned != today {
		t.Fatalf("last planned = %v, want %s", rs[0].LastPlanned, today)
	}
	must(t, s.ClearSlot(today, "lunch"))
	if rs, _ := s.ListRecipes(); rs[0].LastPlanned != nil {
		t.Errorf("last planned after removal = %v, want nil", *rs[0].LastPlanned)
	}
}

func TestRestoringAListedSlotKeepsItListedOnce(t *testing.T) {
	s := newStore(t)
	a := mustRecipe(t, s, "A", "apple")
	must(t, s.SetSlotRecipe(today, "lunch", a.ID))
	_, err := s.Validate(today)
	must(t, err)
	must(t, s.ClearSlot(today, "lunch"))
	must(t, s.SetSlotRecipe(today, "lunch", a.ID))
	must(t, s.SetSlotStatus(today, "lunch", StatusListed))
	p, _ := s.Plan(today)
	if p.Days[0].Lunch.Status != StatusListed || p.Pending != 0 {
		t.Errorf("restored slot = %+v, pending %d", p.Days[0].Lunch, p.Pending)
	}
}

func TestClientIDs(t *testing.T) {
	s := newStore(t)
	id := "6f1c1b3e-5a0e-4a53-9d0e-0b6f7a8f2c11"
	r, err := s.CreateRecipe(RecipeInput{ID: id, Title: "Soup"})
	if err != nil || r.ID != id {
		t.Fatalf("CreateRecipe with an id: %+v, %v", r, err)
	}
	if _, err := s.CreateRecipe(RecipeInput{ID: "x", Title: "Soup"}); !isInvalid(err) {
		t.Fatalf("bad id: err = %v, want a validation error", err)
	}
	if a, err := s.CreateAisle("", "Butcher"); err != nil || a.ID == "" {
		t.Fatalf("CreateAisle without id: %+v, %v", a, err)
	}
	must(t, s.AddExtra("", "Piles"))
	if err := s.AddExtra("x", "Piles 2"); !isInvalid(err) {
		t.Fatalf("bad extra id: err = %v", err)
	}
}
