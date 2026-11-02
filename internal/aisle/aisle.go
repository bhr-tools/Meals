// Package aisle normalizes ingredient names and guesses their supermarket
// aisle from a built-in keyword dictionary.
package aisle

import (
	_ "embed"
	"sort"
	"strings"
	"unicode"
)

//go:embed dictionary.txt
var rawDictionary string

type entry struct {
	words []string
	key   string
}

var entries = parseDictionary(rawDictionary)

var accents = strings.NewReplacer(
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"à", "a", "â", "a", "ä", "a",
	"î", "i", "ï", "i",
	"ô", "o", "ö", "o",
	"ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "œ", "oe", "æ", "ae",
)

// Normalize reduces a name to a comparable form: lower case, no accents or
// punctuation, single spaces and a naive singular, so that "Courgettes" and
// "courgette" are the same ingredient.
func Normalize(s string) string {
	s = accents.Replace(strings.ToLower(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, s)
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = singular(w)
	}
	return strings.Join(words, " ")
}

func singular(w string) string {
	if len(w) > 3 && strings.HasSuffix(w, "s") &&
		!strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is") {
		return w[:len(w)-1]
	}
	return w
}

// Classify returns the dictionary key (such as "produce") of the aisle that
// holds the normalized ingredient name, if the dictionary knows it. Keywords
// match whole words; the longest keyword wins.
func Classify(norm string) (string, bool) {
	words := strings.Fields(norm)
	for _, e := range entries {
		if containsRun(words, e.words) {
			return e.key, true
		}
	}
	return "", false
}

func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		match := true
		for j, w := range run {
			if words[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// parseDictionary reads "key: keyword, keyword, ..." lines ('#' starts a
// comment) and orders the keywords from most to least specific.
func parseDictionary(raw string) []entry {
	var out []entry
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		key, list, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		for _, kw := range strings.Split(list, ",") {
			if norm := Normalize(kw); norm != "" {
				out = append(out, entry{words: strings.Fields(norm), key: strings.TrimSpace(key)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].words) != len(out[j].words) {
			return len(out[i].words) > len(out[j].words)
		}
		return len(strings.Join(out[i].words, " ")) > len(strings.Join(out[j].words, " "))
	})
	return out
}
