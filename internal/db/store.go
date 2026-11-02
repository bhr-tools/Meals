// Package db persists recipes, the meal plan, the shopping list and
// authentication data in a single SQLite file.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when the requested row does not exist.
var ErrNotFound = errors.New("not found")

// ValidationError reports rejected input. Its message is meant for the user.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single connection serializes every statement, which rules out
	// "database is locked" errors for a personal app.
	sqlDB.SetMaxOpenConns(1)

	s := &Store{db: sqlDB}
	if err := s.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Ping checks that the database answers.
func (s *Store) Ping() error { return s.db.Ping() }

// tx runs fn inside a transaction, committed only if fn returns nil.
func (s *Store) tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// migrations are applied in order; PRAGMA user_version records how many ran.
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
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		err := s.tx(func(tx *sql.Tx) error {
			if _, err := tx.Exec(migrations[i]); err != nil {
				return err
			}
			_, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}

func newID() string { return uuid.NewString() }

func exists(q querier, query string, args ...any) (bool, error) {
	var one int
	err := q.QueryRow(query, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func validDate(s string) error {
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return invalid("Invalid date %q, expected YYYY-MM-DD", s)
	}
	return nil
}
