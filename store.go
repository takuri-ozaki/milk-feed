package main

import (
	"database/sql"
	_ "embed"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

const (
	feedingKeepN   = 7
	CaregiverAName = "パパ"
	CaregiverBName = "ママ"
)

type Settings struct {
	IntervalMinMinutes int
	IntervalMaxMinutes int
}

type Feeding struct {
	ID    int64
	FedAt time.Time
}

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.ensureDefaultSettings(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ensureDefaultSettings() error {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE id = 1`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO settings (id, interval_min_minutes, interval_max_minutes) VALUES (1, ?, ?)`,
		180, 240,
	)
	return err
}

func (s *Store) GetSettings() (Settings, error) {
	var st Settings
	err := s.db.QueryRow(
		`SELECT interval_min_minutes, interval_max_minutes FROM settings WHERE id = 1`,
	).Scan(&st.IntervalMinMinutes, &st.IntervalMaxMinutes)
	return st, err
}

func (s *Store) UpdateSettings(st Settings) error {
	if st.IntervalMinMinutes <= 0 || st.IntervalMaxMinutes <= 0 {
		return errors.New("interval must be positive")
	}
	if st.IntervalMinMinutes > st.IntervalMaxMinutes {
		return errors.New("interval min must be <= max")
	}
	_, err := s.db.Exec(
		`UPDATE settings SET interval_min_minutes = ?, interval_max_minutes = ? WHERE id = 1`,
		st.IntervalMinMinutes, st.IntervalMaxMinutes,
	)
	return err
}

func (s *Store) RecentFeedings(limit int) ([]Feeding, error) {
	rows, err := s.db.Query(`SELECT id, fed_at_unix FROM feedings ORDER BY fed_at_unix DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feeding
	for rows.Next() {
		var f Feeding
		var unix int64
		if err := rows.Scan(&f.ID, &unix); err != nil {
			return nil, err
		}
		f.FedAt = time.Unix(unix, 0)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) LatestFeedingTime() (time.Time, bool, error) {
	var unix sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(fed_at_unix) FROM feedings`).Scan(&unix)
	if err != nil {
		return time.Time{}, false, err
	}
	if !unix.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(unix.Int64, 0), true, nil
}

// AddFeedingAndShift inserts a new feeding record, trims older records to keep
// only the latest feedingKeepN, and shifts assignments down by one (slot 1 is
// consumed; slot N>1 becomes N-1). All in one transaction.
func (s *Store) AddFeedingAndShift(t time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO feedings (fed_at_unix) VALUES (?)`, t.Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM feedings WHERE id NOT IN (SELECT id FROM feedings ORDER BY fed_at_unix DESC LIMIT ?)`,
		feedingKeepN,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM assignments WHERE slot_index = 1`); err != nil {
		return err
	}
	// Two-step shift via a high offset to avoid mid-statement UNIQUE
	// constraint violations on (slot_index, caregiver).
	if _, err := tx.Exec(`UPDATE assignments SET slot_index = slot_index + 1000000 WHERE slot_index > 1`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE assignments SET slot_index = slot_index - 1000001 WHERE slot_index > 1000000`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM next_adjustment WHERE id = 1`); err != nil {
		return err
	}
	return tx.Commit()
}

type NextAdjustment struct {
	Target time.Time
	Reason string
}

func (s *Store) GetNextAdjustment() (NextAdjustment, bool, error) {
	var unix int64
	var reason string
	err := s.db.QueryRow(`SELECT target_unix, reason FROM next_adjustment WHERE id = 1`).Scan(&unix, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return NextAdjustment{}, false, nil
	}
	if err != nil {
		return NextAdjustment{}, false, err
	}
	return NextAdjustment{Target: time.Unix(unix, 0), Reason: reason}, true, nil
}

func (s *Store) SetNextAdjustment(target time.Time, reason string) error {
	_, err := s.db.Exec(
		`INSERT INTO next_adjustment (id, target_unix, reason) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET target_unix = excluded.target_unix, reason = excluded.reason`,
		target.Unix(), reason,
	)
	return err
}

func (s *Store) ClearNextAdjustment() error {
	_, err := s.db.Exec(`DELETE FROM next_adjustment WHERE id = 1`)
	return err
}

// DeleteFeeding removes a feeding record. If the deleted record is the latest
// (i.e. the anchor for the schedule), assignments are shifted up by 1 to
// reverse the shift that happened in AddFeedingAndShift. The original slot 1
// at record time is unrecoverable, so the new slot 1 is left empty (unset).
func (s *Store) DeleteFeeding(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var fedAt int64
	err = tx.QueryRow(`SELECT fed_at_unix FROM feedings WHERE id = ?`, id).Scan(&fedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}

	var maxFedAt sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(fed_at_unix) FROM feedings`).Scan(&maxFedAt); err != nil {
		return err
	}
	isLatest := maxFedAt.Valid && fedAt == maxFedAt.Int64

	if _, err := tx.Exec(`DELETE FROM feedings WHERE id = ?`, id); err != nil {
		return err
	}

	if isLatest {
		// Two-step shift via a high offset (see AddFeedingAndShift).
		if _, err := tx.Exec(`UPDATE assignments SET slot_index = slot_index + 1000000`); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE assignments SET slot_index = slot_index - 999999 WHERE slot_index > 1000000`); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// AssignmentMap[slotIndex][caregiver("a"|"b")] = status("o"|"t"|"x").
type AssignmentMap map[int]map[string]string

func (s *Store) AllAssignments() (AssignmentMap, error) {
	rows, err := s.db.Query(`SELECT slot_index, caregiver, status FROM assignments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := AssignmentMap{}
	for rows.Next() {
		var idx int
		var cg, st string
		if err := rows.Scan(&idx, &cg, &st); err != nil {
			return nil, err
		}
		if out[idx] == nil {
			out[idx] = map[string]string{}
		}
		out[idx][cg] = st
	}
	return out, rows.Err()
}

func (s *Store) SetAssignment(slotIndex int, caregiver, status string) error {
	if status == "none" || status == "" {
		_, err := s.db.Exec(`DELETE FROM assignments WHERE slot_index = ? AND caregiver = ?`, slotIndex, caregiver)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO assignments (slot_index, caregiver, status) VALUES (?, ?, ?)
		 ON CONFLICT(slot_index, caregiver) DO UPDATE SET status = excluded.status`,
		slotIndex, caregiver, status,
	)
	return err
}
