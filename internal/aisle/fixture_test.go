package aisle

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The JavaScript port (web/js/aisle.js) is tested against the same file: the
// Go code is the reference, never change it to match the JavaScript.
// UPDATE_FIXTURES=1 go test ./internal/aisle rewrites the expected values and
// the dictionary from the Go code; read the diff before keeping it.
const fixturePath = "../../tests/fixtures/normalize.json"

type fixture struct {
	Normalize []struct {
		Name string `json:"name"`
		Want string `json:"want"`
	} `json:"normalize"`
	Classify []struct {
		Name string `json:"name"`
		Key  string `json:"key"` // "" when the dictionary does not know the name
	} `json:"classify"`
	Dictionary []struct {
		Words []string `json:"words"`
		Key   string   `json:"key"`
	} `json:"dictionary"`
}

func TestFixture(t *testing.T) {
	b, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_FIXTURES") != "" {
		for i := range f.Normalize {
			f.Normalize[i].Want = Normalize(f.Normalize[i].Name)
		}
		for i := range f.Classify {
			f.Classify[i].Key, _ = Classify(Normalize(f.Classify[i].Name))
		}
		f.Dictionary = nil
		for _, e := range Entries() {
			f.Dictionary = append(f.Dictionary, struct {
				Words []string `json:"words"`
				Key   string   `json:"key"`
			}{e.Words, e.Key})
		}
		out, _ := json.MarshalIndent(f, "", " ")
		if err := os.WriteFile(fixturePath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range f.Normalize {
		if got := Normalize(c.Name); got != c.Want {
			t.Errorf("Normalize(%q) = %q, want %q", c.Name, got, c.Want)
		}
	}
	for _, c := range f.Classify {
		got, _ := Classify(Normalize(c.Name))
		if got != c.Key {
			t.Errorf("Classify(%q) = %q, want %q", c.Name, got, c.Key)
		}
	}
	var dict []struct {
		Words []string
		Key   string
	}
	for _, d := range f.Dictionary {
		dict = append(dict, struct {
			Words []string
			Key   string
		}{d.Words, d.Key})
	}
	var cur []struct {
		Words []string
		Key   string
	}
	for _, e := range Entries() {
		cur = append(cur, struct {
			Words []string
			Key   string
		}{e.Words, e.Key})
	}
	if !reflect.DeepEqual(dict, cur) {
		t.Errorf("the dictionary in %s is out of date: run UPDATE_FIXTURES=1 go test ./internal/aisle", strings.TrimPrefix(fixturePath, "../../"))
	}
}
