package db

import (
	"database/sql"
	"time"

	"github.com/ben/meals/internal/aisle"
)

// syncOpsKept is how long applied changes are remembered.
const syncOpsKept = 30 * 24 * time.Hour

// Snapshot is all the data, as stored, for clients that work offline.
type Snapshot struct {
	Recipes    []SnapRecipe      `json:"recipes"`
	Slots      []SnapSlot        `json:"slots"`
	Aisles     []SnapAisle       `json:"aisles"`     // ordered by position
	Overrides  map[string]string `json:"overrides"`  // norm -> aisle id (ingredient_aisle)
	Checked    []string          `json:"checked"`    // norms (shopping_checked)
	Extras     []SnapExtra       `json:"extras"`     // shopping_extra
	Dictionary []DictEntry       `json:"dictionary"` // aisle.Entries(), most specific first
}

// SnapRecipe is a recipe with its ingredients, in position order.
type SnapRecipe struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Notes       string       `json:"notes"`
	CreatedAt   string       `json:"created_at"`
	Ingredients []SnapIngred `json:"ingredients"`
}

// SnapIngred is an ingredient name and its normalized form.
type SnapIngred struct {
	Name string `json:"name"`
	Norm string `json:"norm"`
}

// SnapSlot is one meal of one day.
type SnapSlot struct {
	Date     string  `json:"date"`
	Meal     string  `json:"meal"`
	RecipeID *string `json:"recipe_id"`
	Text     string  `json:"text"`
	Status   string  `json:"status"`
}

// SnapAisle is a shopping aisle; Key is empty for custom aisles.
type SnapAisle struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// SnapExtra is a hand-written shopping item.
type SnapExtra struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Norm string `json:"norm"`
}

// DictEntry is a dictionary keyword (normalized words) and its aisle key.
type DictEntry struct {
	Words []string `json:"words"`
	Key   string   `json:"key"`
}

// Snapshot reads everything in one transaction, so that the lists are
// consistent with each other.
func (s *Store) Snapshot() (*Snapshot, error) {
	snap := &Snapshot{
		Recipes: []SnapRecipe{}, Slots: []SnapSlot{}, Aisles: []SnapAisle{},
		Overrides: map[string]string{}, Checked: []string{}, Extras: []SnapExtra{}, Dictionary: []DictEntry{},
	}
	err := s.tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id, title, notes, COALESCE(CAST(created_at AS TEXT), '') FROM recipes ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		index := map[string]int{}
		for rows.Next() {
			r := SnapRecipe{Ingredients: []SnapIngred{}}
			if err := rows.Scan(&r.ID, &r.Title, &r.Notes, &r.CreatedAt); err != nil {
				return err
			}
			index[r.ID] = len(snap.Recipes)
			snap.Recipes = append(snap.Recipes, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		ing, err := tx.Query(`SELECT recipe_id, name, norm FROM recipe_ingredients ORDER BY recipe_id, position`)
		if err != nil {
			return err
		}
		defer ing.Close()
		for ing.Next() {
			var id string
			var in SnapIngred
			if err := ing.Scan(&id, &in.Name, &in.Norm); err != nil {
				return err
			}
			if i, ok := index[id]; ok {
				snap.Recipes[i].Ingredients = append(snap.Recipes[i].Ingredients, in)
			}
		}
		if err := ing.Err(); err != nil {
			return err
		}

		slots, err := tx.Query(`SELECT date, meal, recipe_id, text, status FROM slots ORDER BY date, meal`)
		if err != nil {
			return err
		}
		defer slots.Close()
		for slots.Next() {
			var sl SnapSlot
			var recipeID sql.NullString
			if err := slots.Scan(&sl.Date, &sl.Meal, &recipeID, &sl.Text, &sl.Status); err != nil {
				return err
			}
			sl.RecipeID = ptr(recipeID)
			snap.Slots = append(snap.Slots, sl)
		}
		if err := slots.Err(); err != nil {
			return err
		}

		aisles, err := tx.Query(`SELECT id, COALESCE(key, ''), name, position FROM aisles ORDER BY position`)
		if err != nil {
			return err
		}
		defer aisles.Close()
		for aisles.Next() {
			var a SnapAisle
			if err := aisles.Scan(&a.ID, &a.Key, &a.Name, &a.Position); err != nil {
				return err
			}
			snap.Aisles = append(snap.Aisles, a)
		}
		if err := aisles.Err(); err != nil {
			return err
		}

		if snap.Overrides, err = stringMap(tx, `SELECT norm, aisle_id FROM ingredient_aisle`); err != nil {
			return err
		}
		checked, err := tx.Query(`SELECT norm FROM shopping_checked ORDER BY norm`)
		if err != nil {
			return err
		}
		defer checked.Close()
		for checked.Next() {
			var norm string
			if err := checked.Scan(&norm); err != nil {
				return err
			}
			snap.Checked = append(snap.Checked, norm)
		}
		if err := checked.Err(); err != nil {
			return err
		}

		extras, err := tx.Query(`SELECT id, name, norm FROM shopping_extra ORDER BY norm`)
		if err != nil {
			return err
		}
		defer extras.Close()
		for extras.Next() {
			var e SnapExtra
			if err := extras.Scan(&e.ID, &e.Name, &e.Norm); err != nil {
				return err
			}
			snap.Extras = append(snap.Extras, e)
		}
		return extras.Err()
	})
	if err != nil {
		return nil, err
	}
	for _, e := range aisle.Entries() {
		snap.Dictionary = append(snap.Dictionary, DictEntry{Words: e.Words, Key: e.Key})
	}
	return snap, nil
}

// SyncOpStatus returns the recorded outcome of a change already applied.
func (s *Store) SyncOpStatus(id string) (status, msg string, found bool, err error) {
	err = s.db.QueryRow(`SELECT status, error FROM sync_ops WHERE id = ?`, id).Scan(&status, &msg)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	return status, msg, err == nil, err
}

// RecordSyncOp remembers the outcome of a change.
func (s *Store) RecordSyncOp(id, status, msg string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO sync_ops (id, status, error, applied_at) VALUES (?, ?, ?, ?)`,
		id, status, msg, time.Now().UTC())
	return err
}

// PruneSyncOps forgets changes applied long ago.
func (s *Store) PruneSyncOps() error {
	_, err := s.db.Exec(`DELETE FROM sync_ops WHERE applied_at < ?`, time.Now().UTC().Add(-syncOpsKept))
	return err
}
