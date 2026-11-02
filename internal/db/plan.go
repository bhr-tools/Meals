package db

import (
	"database/sql"
	"strings"
	"time"
)

// WindowDays is how many days the plan shows, starting today.
const WindowDays = 7

const maxSlotText = 200

// Meals are the slots of a day, in display order.
var Meals = []string{"lunch", "dinner"}

// Slot states of a recipe: pending (planned, not shopped for), listed
// (validated into the shopping list), done (shopping finished). Text slots
// are always done: they never reach the shopping list.
const (
	StatusPending = "pending"
	StatusListed  = "listed"
	StatusDone    = "done"
)

// Slot is one meal of one day: a recipe or a free text.
type Slot struct {
	RecipeID *string `json:"recipe_id"`
	Title    string  `json:"title,omitempty"`
	Text     string  `json:"text,omitempty"`
	Status   string  `json:"status"`
}

// Day holds the two meals of a date; a missing meal is nil.
type Day struct {
	Date   string `json:"date"`
	Lunch  *Slot  `json:"lunch"`
	Dinner *Slot  `json:"dinner"`
}

// Plan is the rolling window of days, with the number of recipe slots
// waiting for validation.
type Plan struct {
	Today   string `json:"today"`
	Days    []Day  `json:"days"`
	Pending int    `json:"pending"`
}

func validMeal(meal string) error {
	for _, m := range Meals {
		if m == meal {
			return nil
		}
	}
	return invalid("Unknown meal %q", meal)
}

// Plan returns WindowDays days starting at today (YYYY-MM-DD).
func (s *Store) Plan(today string) (Plan, error) {
	if err := validDate(today); err != nil {
		return Plan{}, err
	}
	start, _ := time.Parse(time.DateOnly, today)
	end := start.AddDate(0, 0, WindowDays-1).Format(time.DateOnly)

	p := Plan{Today: today, Days: make([]Day, WindowDays)}
	index := map[string]int{}
	for i := range p.Days {
		date := start.AddDate(0, 0, i).Format(time.DateOnly)
		p.Days[i].Date = date
		index[date] = i
	}
	rows, err := s.db.Query(`SELECT s.date, s.meal, s.recipe_id, COALESCE(r.title, ''), s.text, s.status
		FROM slots s LEFT JOIN recipes r ON r.id = s.recipe_id
		WHERE s.date BETWEEN ? AND ?`, today, end)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var date, meal string
		var recipeID sql.NullString
		slot := &Slot{}
		if err := rows.Scan(&date, &meal, &recipeID, &slot.Title, &slot.Text, &slot.Status); err != nil {
			return p, err
		}
		slot.RecipeID = ptr(recipeID)
		day := &p.Days[index[date]]
		if meal == "lunch" {
			day.Lunch = slot
		} else {
			day.Dinner = slot
		}
		if slot.RecipeID != nil && slot.Status == StatusPending {
			p.Pending++
		}
	}
	return p, rows.Err()
}

// SetSlotRecipe puts a recipe in a slot. Choosing the recipe already there
// changes nothing; a different recipe starts pending.
func (s *Store) SetSlotRecipe(date, meal, recipeID string) error {
	if err := validDate(date); err != nil {
		return err
	}
	if err := validMeal(meal); err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		found, err := exists(tx, `SELECT 1 FROM recipes WHERE id = ?`, recipeID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		_, err = tx.Exec(`INSERT INTO slots (date, meal, recipe_id, text, status) VALUES (?, ?, ?, '', 'pending')
			ON CONFLICT(date, meal) DO UPDATE SET recipe_id = excluded.recipe_id, text = '', status = 'pending'
			WHERE slots.recipe_id IS NOT excluded.recipe_id`, date, meal, recipeID)
		return err
	})
}

// SetSlotText puts a free text ("leftovers", "out") in a slot.
func (s *Store) SetSlotText(date, meal, text string) error {
	if err := validDate(date); err != nil {
		return err
	}
	if err := validMeal(meal); err != nil {
		return err
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return invalid("The text cannot be empty")
	}
	if len([]rune(text)) > maxSlotText {
		return invalid("The text is too long (max %d characters)", maxSlotText)
	}
	_, err := s.db.Exec(`INSERT INTO slots (date, meal, recipe_id, text, status) VALUES (?, ?, NULL, ?, 'done')
		ON CONFLICT(date, meal) DO UPDATE SET recipe_id = NULL, text = excluded.text, status = 'done'`, date, meal, text)
	return err
}

// ClearSlot empties a slot; clearing an empty slot is fine.
func (s *Store) ClearSlot(date, meal string) error {
	if err := validDate(date); err != nil {
		return err
	}
	if err := validMeal(meal); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM slots WHERE date = ? AND meal = ?`, date, meal)
	return err
}

// CopyDay replaces the meals of one day by those of another. Copied recipes
// start pending, since they have not been shopped for on the new day.
func (s *Store) CopyDay(from, to string) error {
	if err := validDate(from); err != nil {
		return err
	}
	if err := validDate(to); err != nil {
		return err
	}
	if from == to {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM slots WHERE date = ?`, to); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO slots (date, meal, recipe_id, text, status)
			SELECT ?, meal, recipe_id, text, CASE WHEN recipe_id IS NULL THEN 'done' ELSE 'pending' END
			FROM slots WHERE date = ?`, to, from)
		return err
	})
}

// Validate moves the pending recipes of today and later into the shopping
// list. It returns how many slots were validated.
func (s *Store) Validate(today string) (int, error) {
	if err := validDate(today); err != nil {
		return 0, err
	}
	n := 0
	err := s.tx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE slots SET status = 'listed'
			WHERE status = 'pending' AND recipe_id IS NOT NULL AND date >= ?`, today)
		if err != nil {
			return err
		}
		count, _ := res.RowsAffected()
		n = int(count)
		_, err = tx.Exec(`UPDATE recipes SET last_planned =
			(SELECT MAX(date) FROM slots WHERE recipe_id = recipes.id AND status != 'pending')
			WHERE id IN (SELECT recipe_id FROM slots WHERE recipe_id IS NOT NULL AND status != 'pending')`)
		return err
	})
	return n, err
}

// FinishShopping closes the current list: listed recipes become done, and the
// ticks and hand-written items are cleared.
func (s *Store) FinishShopping() error {
	return s.tx(func(tx *sql.Tx) error {
		for _, q := range []string{
			`UPDATE slots SET status = 'done' WHERE status = 'listed'`,
			`DELETE FROM shopping_checked`,
			`DELETE FROM shopping_extra`,
		} {
			if _, err := tx.Exec(q); err != nil {
				return err
			}
		}
		return nil
	})
}
