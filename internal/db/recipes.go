package db

import (
	"database/sql"
	"strings"

	"github.com/ben/meals/internal/aisle"
)

const (
	maxTitle       = 200
	maxIngredient  = 100
	maxIngredients = 100
	maxNotes       = 20000
)

// Recipe is a title, a list of ingredient names (no quantities) and free
// notes that never reach the shopping list.
type Recipe struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Ingredients []string `json:"ingredients"`
	Notes       string   `json:"notes"`
	LastPlanned *string  `json:"last_planned"`
}

// RecipeInput is what the user submits when creating or editing a recipe.
type RecipeInput struct {
	Title       string   `json:"title"`
	Ingredients []string `json:"ingredients"`
	Notes       string   `json:"notes"`
}

type cleanIngredient struct{ name, norm string }

// clean validates the input and returns its ingredients without blanks or
// duplicates (same normalized name), in their original order.
func (in RecipeInput) clean() (string, []cleanIngredient, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return "", nil, invalid("The title cannot be empty")
	}
	if len([]rune(title)) > maxTitle {
		return "", nil, invalid("The title is too long (max %d characters)", maxTitle)
	}
	if len([]rune(in.Notes)) > maxNotes {
		return "", nil, invalid("The notes are too long (max %d characters)", maxNotes)
	}
	var out []cleanIngredient
	seen := map[string]bool{}
	for _, line := range in.Ingredients {
		name := strings.Join(strings.Fields(line), " ")
		norm := aisle.Normalize(name)
		if norm == "" || seen[norm] {
			continue
		}
		if len([]rune(name)) > maxIngredient {
			return "", nil, invalid("An ingredient is too long (max %d characters)", maxIngredient)
		}
		seen[norm] = true
		out = append(out, cleanIngredient{name, norm})
	}
	if len(out) > maxIngredients {
		return "", nil, invalid("Too many ingredients (max %d)", maxIngredients)
	}
	return title, out, nil
}

// ListRecipes returns every recipe, never-planned and longest-ago first.
func (s *Store) ListRecipes() ([]Recipe, error) {
	rows, err := s.db.Query(`SELECT id, title, notes, last_planned FROM recipes
		ORDER BY last_planned IS NOT NULL, last_planned, title COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recipes := []Recipe{}
	index := map[string]int{}
	for rows.Next() {
		var r Recipe
		var last sql.NullString
		if err := rows.Scan(&r.ID, &r.Title, &r.Notes, &last); err != nil {
			return nil, err
		}
		r.LastPlanned = ptr(last)
		r.Ingredients = []string{}
		index[r.ID] = len(recipes)
		recipes = append(recipes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ing, err := s.db.Query(`SELECT recipe_id, name FROM recipe_ingredients ORDER BY recipe_id, position`)
	if err != nil {
		return nil, err
	}
	defer ing.Close()
	for ing.Next() {
		var id, name string
		if err := ing.Scan(&id, &name); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			recipes[i].Ingredients = append(recipes[i].Ingredients, name)
		}
	}
	return recipes, ing.Err()
}

// GetRecipe returns one recipe.
func (s *Store) GetRecipe(id string) (Recipe, error) {
	return getRecipe(s.db, id)
}

func getRecipe(q querier, id string) (Recipe, error) {
	r := Recipe{Ingredients: []string{}}
	var last sql.NullString
	err := q.QueryRow(`SELECT id, title, notes, last_planned FROM recipes WHERE id = ?`, id).
		Scan(&r.ID, &r.Title, &r.Notes, &last)
	if err == sql.ErrNoRows {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r.LastPlanned = ptr(last)
	rows, err := q.Query(`SELECT name FROM recipe_ingredients WHERE recipe_id = ? ORDER BY position`, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return r, err
		}
		r.Ingredients = append(r.Ingredients, name)
	}
	return r, rows.Err()
}

func writeIngredients(tx *sql.Tx, id string, ingredients []cleanIngredient) error {
	if _, err := tx.Exec(`DELETE FROM recipe_ingredients WHERE recipe_id = ?`, id); err != nil {
		return err
	}
	for i, ing := range ingredients {
		if _, err := tx.Exec(`INSERT INTO recipe_ingredients (recipe_id, position, name, norm) VALUES (?, ?, ?, ?)`,
			id, i, ing.name, ing.norm); err != nil {
			return err
		}
	}
	return nil
}

// CreateRecipe adds a recipe.
func (s *Store) CreateRecipe(in RecipeInput) (Recipe, error) {
	title, ingredients, err := in.clean()
	if err != nil {
		return Recipe{}, err
	}
	id := newID()
	err = s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO recipes (id, title, notes) VALUES (?, ?, ?)`, id, title, in.Notes); err != nil {
			return err
		}
		return writeIngredients(tx, id, ingredients)
	})
	if err != nil {
		return Recipe{}, err
	}
	return s.GetRecipe(id)
}

// UpdateRecipe replaces a recipe's title, ingredients and notes.
func (s *Store) UpdateRecipe(id string, in RecipeInput) (Recipe, error) {
	title, ingredients, err := in.clean()
	if err != nil {
		return Recipe{}, err
	}
	err = s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE recipes SET title = ?, notes = ? WHERE id = ?`, title, in.Notes, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return writeIngredients(tx, id, ingredients)
	})
	if err != nil {
		return Recipe{}, err
	}
	return s.GetRecipe(id)
}

// DeleteRecipe removes a recipe together with the plan slots that use it.
func (s *Store) DeleteRecipe(id string) error {
	res, err := s.db.Exec(`DELETE FROM recipes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func ptr(ns sql.NullString) *string {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	v := ns.String
	return &v
}
