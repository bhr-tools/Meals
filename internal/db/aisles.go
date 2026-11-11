package db

import (
	"database/sql"
	"strings"
)

const maxAisleName = 40

// Aisle is a shopping list section. Default aisles carry a dictionary key.
type Aisle struct {
	ID   string `json:"id"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name"`
}

// OtherAisleKey is the aisle that receives every unrecognized ingredient.
const OtherAisleKey = "other"

func listAisles(q querier) ([]Aisle, error) {
	rows, err := q.Query(`SELECT id, COALESCE(key, ''), name FROM aisles ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	aisles := []Aisle{}
	for rows.Next() {
		var a Aisle
		if err := rows.Scan(&a.ID, &a.Key, &a.Name); err != nil {
			return nil, err
		}
		aisles = append(aisles, a)
	}
	return aisles, rows.Err()
}

// ListAisles returns the aisles in store order.
func (s *Store) ListAisles() ([]Aisle, error) { return listAisles(s.db) }

func cleanAisleName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", invalid("The aisle name cannot be empty")
	}
	if len([]rune(name)) > maxAisleName {
		return "", invalid("The aisle name is too long (max %d characters)", maxAisleName)
	}
	return name, nil
}

// HasAisle reports whether an aisle exists.
func (s *Store) HasAisle(id string) (bool, error) {
	return exists(s.db, `SELECT 1 FROM aisles WHERE id = ?`, id)
}

// CreateAisle adds an aisle at the end of the list, just before Other. id is
// optional: a UUID chosen by the client.
func (s *Store) CreateAisle(id, name string) (Aisle, error) {
	name, err := cleanAisleName(name)
	if err != nil {
		return Aisle{}, err
	}
	id, err = clientID(id)
	if err != nil {
		return Aisle{}, err
	}
	a := Aisle{ID: id, Name: name}
	err = s.tx(func(tx *sql.Tx) error {
		var otherPos int
		if err := tx.QueryRow(`SELECT position FROM aisles WHERE key = ?`, OtherAisleKey).Scan(&otherPos); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE aisles SET position = position + 1 WHERE position >= ?`, otherPos); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO aisles (id, key, name, position) VALUES (?, NULL, ?, ?)`, a.ID, name, otherPos)
		return err
	})
	return a, err
}

// RenameAisle renames an aisle; its dictionary key is kept.
func (s *Store) RenameAisle(id, name string) error {
	name, err := cleanAisleName(name)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE aisles SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAisle removes an aisle; its ingredients fall back to the dictionary
// guess, or to Other. Other itself cannot be deleted.
func (s *Store) DeleteAisle(id string) error {
	var key sql.NullString
	err := s.db.QueryRow(`SELECT key FROM aisles WHERE id = ?`, id).Scan(&key)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if key.String == OtherAisleKey {
		return invalid("The Other aisle cannot be deleted")
	}
	_, err = s.db.Exec(`DELETE FROM aisles WHERE id = ?`, id)
	return err
}

// ReorderAisles sets the aisle order. ids must list every aisle exactly once.
func (s *Store) ReorderAisles(ids []string) error {
	return s.tx(func(tx *sql.Tx) error {
		current, err := listAisles(tx)
		if err != nil {
			return err
		}
		if len(ids) != len(current) {
			return invalid("The new order must list every aisle")
		}
		known := map[string]bool{}
		for _, a := range current {
			known[a.ID] = true
		}
		for i, id := range ids {
			if !known[id] {
				return invalid("The new order must list every aisle exactly once")
			}
			delete(known, id)
			if _, err := tx.Exec(`UPDATE aisles SET position = ? WHERE id = ?`, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}
