package aisle

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Courgettes":      "courgette",
		"  Pâtes  ":       "pate",
		"Pommes de terre": "pomme de terre",
		"Crème fraîche":   "creme fraiche",
		"œufs":            "oeuf",
		"petits pois":     "petit pois",
		"Riz":             "riz",
		"d'ail":           "d ail",
		"Couscous":        "couscous",
		"":                "",
		"  ":              "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"courgettes":           "produce",
		"poulet rôti":          "meat-fish",
		"lait":                 "dairy",
		"lait de coco":         "pantry", // longer keyword beats "lait"
		"thon en boîte":        "pantry", // beats "thon"
		"petits pois":          "produce",
		"petits pois surgelés": "frozen",
		"spaghetti":            "pantry",
		"sauce soja":           "spices-sauces",
		"papier toilette":      "other",
		"tail":                 "", // "ail" must not match inside a word
	}
	for in, want := range cases {
		got, ok := Classify(Normalize(in))
		if want == "" {
			if ok {
				t.Errorf("Classify(%q) = %q, want no match", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("Classify(%q) = %q (%v), want %q", in, got, ok, want)
		}
	}
}

// Entries must list the keywords in the order Classify tries them: the web
// app classifies with that list.
func TestEntriesFollowClassifyOrder(t *testing.T) {
	entries := Entries()
	if len(entries) == 0 {
		t.Fatal("empty dictionary")
	}
	for _, e := range entries {
		want := ""
		for _, c := range entries {
			if containsRun(e.Words, c.Words) {
				want = c.Key
				break
			}
		}
		if got, ok := Classify(strings.Join(e.Words, " ")); !ok || got != want {
			t.Errorf("Classify(%q) = %q, %v; first matching entry says %q", e.Words, got, ok, want)
		}
	}
	entries[0].Words[0] = "changed"
	if Entries()[0].Words[0] == "changed" {
		t.Error("Entries returns the internal slice")
	}
}
