package db

import (
	"database/sql"
	"sort"
	"strings"

	"github.com/ben/meals/internal/aisle"
)

// RecipeRef says that a recipe needs an ingredient, once per planned meal.
type RecipeRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
}

// ShoppingItem is one line of the list. Key is the normalized name used to
// tick it or change its aisle.
type ShoppingItem struct {
	Name    string      `json:"name"`
	Key     string      `json:"key"`
	Checked bool        `json:"checked"`
	ExtraID string      `json:"extra_id,omitempty"`
	Recipes []RecipeRef `json:"recipes"`
}

// ShoppingGroup is the items of one aisle.
type ShoppingGroup struct {
	AisleID string         `json:"aisle_id"`
	Aisle   string         `json:"aisle"`
	Items   []ShoppingItem `json:"items"`
}

// Shopping is the current list: the ingredients of the listed recipes plus
// the hand-written items, grouped by aisle.
type Shopping struct {
	Groups    []ShoppingGroup `json:"groups"`
	Total     int             `json:"total"`
	Remaining int             `json:"remaining"`
}

// Shopping builds the current list.
func (s *Store) Shopping() (Shopping, error) {
	items := map[string]*ShoppingItem{}
	get := func(norm, name string) *ShoppingItem {
		it := items[norm]
		if it == nil {
			it = &ShoppingItem{Name: name, Key: norm, Recipes: []RecipeRef{}}
			items[norm] = it
		}
		return it
	}

	rows, err := s.db.Query(`SELECT ri.norm, MIN(ri.name), r.id, r.title, COUNT(*)
		FROM slots s
		JOIN recipes r ON r.id = s.recipe_id
		JOIN recipe_ingredients ri ON ri.recipe_id = r.id
		WHERE s.status = 'listed'
		GROUP BY ri.norm, r.id
		ORDER BY r.title COLLATE NOCASE`)
	if err != nil {
		return Shopping{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var norm, name string
		var ref RecipeRef
		if err := rows.Scan(&norm, &name, &ref.ID, &ref.Title, &ref.Count); err != nil {
			return Shopping{}, err
		}
		it := get(norm, name)
		it.Recipes = append(it.Recipes, ref)
	}
	if err := rows.Err(); err != nil {
		return Shopping{}, err
	}

	extras, err := s.db.Query(`SELECT id, name, norm FROM shopping_extra`)
	if err != nil {
		return Shopping{}, err
	}
	defer extras.Close()
	for extras.Next() {
		var id, name, norm string
		if err := extras.Scan(&id, &name, &norm); err != nil {
			return Shopping{}, err
		}
		get(norm, name).ExtraID = id
	}
	if err := extras.Err(); err != nil {
		return Shopping{}, err
	}

	checked, err := stringSet(s.db, `SELECT norm FROM shopping_checked`)
	if err != nil {
		return Shopping{}, err
	}
	overrides, err := stringMap(s.db, `SELECT norm, aisle_id FROM ingredient_aisle`)
	if err != nil {
		return Shopping{}, err
	}
	aisles, err := s.ListAisles()
	if err != nil {
		return Shopping{}, err
	}
	byKey := map[string]Aisle{}
	byID := map[string]Aisle{}
	for _, a := range aisles {
		byID[a.ID] = a
		if a.Key != "" {
			byKey[a.Key] = a
		}
	}
	resolve := func(norm string) Aisle {
		if a, ok := byID[overrides[norm]]; ok {
			return a
		}
		if key, ok := aisle.Classify(norm); ok {
			if a, ok := byKey[key]; ok {
				return a
			}
		}
		return byKey[OtherAisleKey]
	}

	groups := map[string]*ShoppingGroup{}
	out := Shopping{Groups: []ShoppingGroup{}}
	for norm, it := range items {
		it.Checked = checked[norm]
		a := resolve(norm)
		g := groups[a.ID]
		if g == nil {
			g = &ShoppingGroup{AisleID: a.ID, Aisle: a.Name}
			groups[a.ID] = g
		}
		g.Items = append(g.Items, *it)
		out.Total++
		if !it.Checked {
			out.Remaining++
		}
	}
	for _, a := range aisles {
		g := groups[a.ID]
		if g == nil {
			continue
		}
		sort.Slice(g.Items, func(i, j int) bool {
			if g.Items[i].Checked != g.Items[j].Checked {
				return !g.Items[i].Checked
			}
			return g.Items[i].Key < g.Items[j].Key
		})
		out.Groups = append(out.Groups, *g)
	}
	return out, nil
}

// SetChecked ticks or unticks an ingredient, by name.
func (s *Store) SetChecked(name string, checked bool) error {
	norm := aisle.Normalize(name)
	if norm == "" {
		return invalid("The name cannot be empty")
	}
	var err error
	if checked {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO shopping_checked (norm) VALUES (?)`, norm)
	} else {
		_, err = s.db.Exec(`DELETE FROM shopping_checked WHERE norm = ?`, norm)
	}
	return err
}

// AddExtra adds a hand-written item to the list. id is optional: a UUID
// chosen by the client.
func (s *Store) AddExtra(id, name string) error {
	name = strings.Join(strings.Fields(name), " ")
	norm := aisle.Normalize(name)
	if norm == "" {
		return invalid("The name cannot be empty")
	}
	if len([]rune(name)) > maxIngredient {
		return invalid("The name is too long (max %d characters)", maxIngredient)
	}
	id, err := clientID(id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO shopping_extra (id, name, norm) VALUES (?, ?, ?)`, id, name, norm)
	return err
}

// DeleteExtra removes a hand-written item.
func (s *Store) DeleteExtra(id string) error {
	_, err := s.db.Exec(`DELETE FROM shopping_extra WHERE id = ?`, id)
	return err
}

// SetIngredientAisle remembers the aisle of an ingredient, overriding the
// dictionary. An empty aisleID forgets the choice.
func (s *Store) SetIngredientAisle(name, aisleID string) error {
	norm := aisle.Normalize(name)
	if norm == "" {
		return invalid("The name cannot be empty")
	}
	if aisleID == "" {
		_, err := s.db.Exec(`DELETE FROM ingredient_aisle WHERE norm = ?`, norm)
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		found, err := exists(tx, `SELECT 1 FROM aisles WHERE id = ?`, aisleID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		_, err = tx.Exec(`INSERT INTO ingredient_aisle (norm, aisle_id) VALUES (?, ?)
			ON CONFLICT(norm) DO UPDATE SET aisle_id = excluded.aisle_id`, norm, aisleID)
		return err
	})
}

func stringSet(q querier, query string) (map[string]bool, error) {
	rows, err := q.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		set[v] = true
	}
	return set, rows.Err()
}

func stringMap(q querier, query string) (map[string]string, error) {
	rows, err := q.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}
