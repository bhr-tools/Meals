package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben/meals/internal/db"
)

func newServer(t *testing.T, noAuth bool) http.Handler {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "meals.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return New(store, noAuth).Handler()
}

func call(t *testing.T, h http.Handler, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestPlanToShoppingFlow(t *testing.T) {
	h := newServer(t, true)

	rec := call(t, h, "POST", "/api/recipes", `{"title":"Gratin","ingredients":["courgettes","crème"],"notes":"bake 30 min"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create recipe: %d %s", rec.Code, rec.Body)
	}
	recipe := decodeBody[db.Recipe](t, rec)

	if rec := call(t, h, "PUT", "/api/slots/2026-11-02/dinner", `{"recipe_id":"`+recipe.ID+`"}`); rec.Code != 200 {
		t.Fatalf("set slot: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, h, "PUT", "/api/slots/2026-11-02/lunch", `{"text":"leftovers"}`); rec.Code != 200 {
		t.Fatalf("set text: %d %s", rec.Code, rec.Body)
	}
	plan := decodeBody[db.Plan](t, call(t, h, "GET", "/api/plan?today=2026-11-02", ""))
	if plan.Pending != 1 || plan.Days[0].Dinner.Title != "Gratin" || plan.Days[0].Lunch.Text != "leftovers" {
		t.Fatalf("plan = %+v", plan)
	}

	if list := decodeBody[db.Shopping](t, call(t, h, "GET", "/api/shopping", "")); list.Total != 0 {
		t.Fatalf("pending recipe on the list: %+v", list)
	}
	rec = call(t, h, "POST", "/api/plan/validate", `{"today":"2026-11-02"}`)
	if got := decodeBody[map[string]int](t, rec)["validated"]; got != 1 {
		t.Fatalf("validated = %d", got)
	}
	list := decodeBody[db.Shopping](t, call(t, h, "GET", "/api/shopping", ""))
	if list.Total != 2 || list.Groups[0].Items[0].Recipes[0].Title != "Gratin" {
		t.Fatalf("list = %+v", list)
	}

	if rec := call(t, h, "PUT", "/api/shopping/checked", `{"name":"courgette","checked":true}`); rec.Code != 200 {
		t.Fatalf("check: %d", rec.Code)
	}
	if rec := call(t, h, "POST", "/api/shopping/done", `{}`); rec.Code != 200 {
		t.Fatalf("done: %d", rec.Code)
	}
	if list := decodeBody[db.Shopping](t, call(t, h, "GET", "/api/shopping", "")); list.Total != 0 {
		t.Fatalf("list after done: %+v", list)
	}
}

func TestErrors(t *testing.T) {
	h := newServer(t, true)
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/api/recipes", `{"title":""}`, 400},
		{"POST", "/api/recipes", `not json`, 400},
		{"PUT", "/api/recipes/nope", `{"title":"x"}`, 404},
		{"PUT", "/api/slots/2026-11-02/dinner", `{"recipe_id":"nope"}`, 404},
		{"PUT", "/api/slots/2026-11-02/brunch", `{"text":"x"}`, 400},
		{"GET", "/api/plan?today=bad", ``, 400},
		{"DELETE", "/api/aisles/other", ``, 400},
		{"GET", "/api/unknown", ``, 404},
	} {
		if rec := call(t, h, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body)
		}
	}
}

func TestAuthAndCSRF(t *testing.T) {
	h := newServer(t, false)
	// Until a password is set the instance is open.
	if rec := call(t, h, "GET", "/api/recipes", ""); rec.Code != 200 {
		t.Fatalf("open instance: %d", rec.Code)
	}
	rec := call(t, h, "POST", "/api/auth/setup", `{"password":"hunter22"}`)
	if rec.Code != 200 || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("setup: %d", rec.Code)
	}
	cookie := rec.Result().Cookies()[0].String()
	if rec := call(t, h, "GET", "/api/recipes", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: %d", rec.Code)
	}
	if rec := call(t, h, "GET", "/api/recipes", "", "Cookie", cookie); rec.Code != 200 {
		t.Errorf("with session: %d", rec.Code)
	}
	if rec := call(t, h, "POST", "/api/recipes", `{"title":"x"}`, "Cookie", cookie, "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site write: %d", rec.Code)
	}
}
