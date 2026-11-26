// Package db persists recipes, the meal plan, the shopping list and
// authentication data in a single SQLite file.
package db

import (
	"database/sql"
	"time"

	"github.com/bhr-tools/kit/httpx"
	kitstore "github.com/bhr-tools/kit/store"
)

// ErrNotFound is returned when the requested row does not exist (the kit's,
// so httpx.Fail answers 404).
var ErrNotFound = httpx.ErrNotFound

// ValidationError reports rejected input. Its message is meant for the user.
type ValidationError = httpx.ValidationError

func invalid(format string, args ...any) error {
	return httpx.Invalid(format, args...)
}

// Store is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// querier is implemented by both *sql.DB and *sql.Tx.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Open opens (creating it if needed) the database at path and migrates it.
func Open(path string) (*Store, error) {
	sqlDB, err := kitstore.Open(path, migrations)
	if err != nil {
		return nil, err
	}
	return &Store{db: sqlDB}, nil
}

// DB is the database, shared with the kit's auth and offline sync.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks that the database answers.
func (s *Store) Ping() error { return s.db.Ping() }

// tx runs fn inside a transaction, committed only if fn returns nil.
func (s *Store) tx(fn func(tx *sql.Tx) error) error { return kitstore.Tx(s.db, fn) }

// migrations are applied in order by the kit (PRAGMA user_version records
// how many ran).
// Never edit a released migration: append a new one instead.
var migrations = []string{
	// 1: base schema.
	`CREATE TABLE recipes (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		notes TEXT NOT NULL DEFAULT '',
		last_planned TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE recipe_ingredients (
		recipe_id TEXT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
		position INTEGER NOT NULL,
		name TEXT NOT NULL,
		norm TEXT NOT NULL,
		PRIMARY KEY (recipe_id, position)
	);
	CREATE INDEX idx_recipe_ingredients_norm ON recipe_ingredients(norm);

	-- A slot holds a recipe or a free text. Recipe slots go pending -> listed
	-- (validated into the shopping list) -> done (shopping finished).
	CREATE TABLE slots (
		date TEXT NOT NULL,
		meal TEXT NOT NULL CHECK (meal IN ('lunch', 'dinner')),
		recipe_id TEXT REFERENCES recipes(id) ON DELETE CASCADE,
		text TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'listed', 'done')),
		PRIMARY KEY (date, meal)
	);
	CREATE INDEX idx_slots_status ON slots(status);

	-- key ties a default aisle to the dictionary; custom aisles have none.
	CREATE TABLE aisles (
		id TEXT PRIMARY KEY,
		key TEXT UNIQUE,
		name TEXT NOT NULL,
		position INTEGER NOT NULL
	);
	INSERT INTO aisles (id, key, name, position) VALUES
		('produce', 'produce', 'Produce', 0),
		('bakery', 'bakery', 'Bakery', 1),
		('meat-fish', 'meat-fish', 'Meat & fish', 2),
		('dairy', 'dairy', 'Dairy & eggs', 3),
		('frozen', 'frozen', 'Frozen', 4),
		('pantry', 'pantry', 'Pantry', 5),
		('spices-sauces', 'spices-sauces', 'Spices & sauces', 6),
		('drinks', 'drinks', 'Drinks', 7),
		('other', 'other', 'Other', 8);

	-- The user's own aisle choices, stronger than the dictionary.
	CREATE TABLE ingredient_aisle (
		norm TEXT PRIMARY KEY,
		aisle_id TEXT NOT NULL REFERENCES aisles(id) ON DELETE CASCADE
	);

	CREATE TABLE shopping_checked (norm TEXT PRIMARY KEY);
	CREATE TABLE shopping_extra (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		norm TEXT NOT NULL UNIQUE
	);

	CREATE TABLE auth_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	CREATE TABLE auth_sessions (
		token TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`,

	// 2: changes applied by the offline sync (see sync.go), so that a change
	// sent twice is applied once.
	`CREATE TABLE IF NOT EXISTS sync_ops (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		error TEXT NOT NULL DEFAULT '',
		applied_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_sync_ops_applied ON sync_ops(applied_at);`,
}

func newID() string { return kitstore.NewID() }

// clientID returns id when it is a UUID chosen by a client (offline
// creation), or a new id when id is empty.
func clientID(id string) (string, error) { return kitstore.ClientID(id) }

func exists(q querier, query string, args ...any) (bool, error) {
	return kitstore.Exists(q, query, args...)
}

func validDate(s string) error {
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return invalid("Invalid date %q, expected YYYY-MM-DD", s)
	}
	return nil
}
