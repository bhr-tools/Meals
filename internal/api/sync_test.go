package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type syncAnswer struct {
	Results  []syncResult `json:"results"`
	ETag     string       `json:"etag"`
	Snapshot struct {
		Recipes []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"recipes"`
		Slots []struct {
			Date, Meal, Status string
			RecipeID           *string `json:"recipe_id"`
		} `json:"slots"`
		Aisles []struct {
			ID, Key string
		} `json:"aisles"`
		Checked    []string          `json:"checked"`
		Extras     []map[string]any  `json:"extras"`
		Overrides  map[string]string `json:"overrides"`
		Dictionary []struct {
			Words []string
			Key   string
		} `json:"dictionary"`
	} `json:"snapshot"`
}

func mkOp(id, typ string, args any) map[string]any {
	return map[string]any{"id": id, "type": typ, "args": args}
}

func postSync(t *testing.T, h http.Handler, ops ...map[string]any) syncAnswer {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"ops": ops})
	rec := call(t, h, "POST", "/api/sync", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/sync = %d %s", rec.Code, rec.Body)
	}
	return decodeBody[syncAnswer](t, rec)
}

func TestSyncSnapshot(t *testing.T) {
	h := newServer(t, true)
	rec := call(t, h, "GET", "/api/sync", "")
	if rec.Code != 200 {
		t.Fatalf("GET /api/sync = %d", rec.Code)
	}
	first := decodeBody[syncAnswer](t, rec)
	if rec.Header().Get("ETag") != first.ETag || first.ETag == "" {
		t.Fatalf("ETag header %q, body %q", rec.Header().Get("ETag"), first.ETag)
	}
	want := []string{"produce", "bakery", "meat-fish", "dairy", "frozen", "pantry", "spices-sauces", "drinks", "other"}
	if len(first.Snapshot.Aisles) != len(want) {
		t.Fatalf("%d aisles, want %d", len(first.Snapshot.Aisles), len(want))
	}
	for i, a := range first.Snapshot.Aisles {
		if a.Key != want[i] {
			t.Errorf("aisle %d = %q, want %q", i, a.Key, want[i])
		}
	}
	if len(first.Snapshot.Dictionary) == 0 {
		t.Error("empty dictionary")
	}
	var raw struct {
		Snapshot map[string]json.RawMessage `json:"snapshot"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"recipes", "slots", "aisles", "checked", "extras", "dictionary"} {
		if raw.Snapshot[k][0] != '[' {
			t.Errorf("%s = %.20s, want an array", k, raw.Snapshot[k])
		}
	}
	if string(raw.Snapshot["overrides"]) != "{}" {
		t.Errorf("overrides = %s, want {}", raw.Snapshot["overrides"])
	}

	if rec := call(t, h, "GET", "/api/sync", "", "If-None-Match", first.ETag); rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match: status %d, want 304", rec.Code)
	}
	call(t, h, "POST", "/api/recipes", `{"title":"Soup"}`)
	if second := decodeBody[syncAnswer](t, call(t, h, "GET", "/api/sync", "")); second.ETag == first.ETag {
		t.Error("ETag did not change after a new recipe")
	}
}

func TestSyncAppliesInOrderOnce(t *testing.T) {
	h := newServer(t, true)
	recipe, extra := uuid.NewString(), uuid.NewString()
	ops := []map[string]any{
		mkOp(uuid.NewString(), "recipe.create", map[string]any{"id": recipe, "title": "Gratin", "ingredients": []string{"Courgettes", "Tomate"}}),
		mkOp(uuid.NewString(), "slot.set_recipe", map[string]any{"date": "2099-01-01", "meal": "lunch", "recipe_id": recipe}),
		mkOp(uuid.NewString(), "plan.validate", map[string]any{"today": "2099-01-01"}),
		mkOp(uuid.NewString(), "shopping.check", map[string]any{"name": "courgette", "checked": true}),
		mkOp(uuid.NewString(), "shopping.add_extra", map[string]any{"id": extra, "name": "Piles"}),
	}
	check := func(out syncAnswer) {
		t.Helper()
		for _, r := range out.Results {
			if r.Status != "ok" {
				t.Fatalf("result %+v", r)
			}
		}
		if len(out.Snapshot.Recipes) != 1 || out.Snapshot.Recipes[0].ID != recipe {
			t.Fatalf("recipes = %+v", out.Snapshot.Recipes)
		}
		if len(out.Snapshot.Extras) != 1 || out.Snapshot.Extras[0]["id"] != extra {
			t.Fatalf("extras = %+v", out.Snapshot.Extras)
		}
		if len(out.Snapshot.Slots) != 1 || out.Snapshot.Slots[0].Status != "listed" {
			t.Fatalf("slots = %+v", out.Snapshot.Slots)
		}
		if fmt.Sprint(out.Snapshot.Checked) != "[courgette]" {
			t.Fatalf("checked = %v", out.Snapshot.Checked)
		}
	}
	check(postSync(t, h, ops...))
	check(postSync(t, h, ops...))
}

func TestSyncStatuses(t *testing.T) {
	h := newServer(t, true)
	out := postSync(t, h,
		mkOp(uuid.NewString(), "recipe.update", map[string]any{"id": uuid.NewString(), "title": "x"}),
		mkOp(uuid.NewString(), "recipe.create", map[string]any{"id": uuid.NewString(), "title": " "}),
		mkOp(uuid.NewString(), "aisle.delete", map[string]any{"id": "other"}),
		mkOp("nope", "shopping.done", map[string]any{}),
		mkOp(uuid.NewString(), "meal.fly", map[string]any{}),
		mkOp(uuid.NewString(), "recipe.create", map[string]any{"id": "x", "title": "T"}),
		mkOp(uuid.NewString(), "aisle.create", map[string]any{"id": uuid.NewString(), "name": "Butcher"}),
	)
	want := []string{"not_found", "invalid", "invalid", "invalid", "invalid", "invalid", "ok"}
	for i, w := range want {
		if out.Results[i].Status != w {
			t.Errorf("result %d = %+v, want %s", i, out.Results[i], w)
		}
	}
	if len(out.Snapshot.Aisles) != 10 {
		t.Errorf("%d aisles, want 10 (the custom one is added)", len(out.Snapshot.Aisles))
	}
	if last := out.Snapshot.Aisles[9]; last.Key != "other" {
		t.Errorf("last aisle %+v, want other", last)
	}
}
