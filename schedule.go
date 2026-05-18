package main

import (
	"fmt"
	"time"
)

const scheduleSlotCount = 10

type Slot struct {
	Index    int
	Start    time.Time
	End      time.Time
	StatusA  string // "o" / "t" / "x" / ""
	StatusB  string
}

// BuildSchedule computes the upcoming slot windows.
//
// Each slot has a fixed width of (maxMinutes - minMinutes) and is shifted by
// minMinutes from the previous one. Slot N (1..count) spans
// [base + N*minMinutes, base + (N-1)*minMinutes + maxMinutes], where base is
// the effective anchor.
//
// Without adjustedNext: base = anchor (the latest feeding time).
// With adjustedNext: base = adjustedNext - minMinutes, so slot 1 starts at
// adjustedNext and has the same (max-min) width as later slots.
func BuildSchedule(anchor time.Time, minMinutes, maxMinutes, count int, asn AssignmentMap, adjustedNext *time.Time) []Slot {
	base := anchor
	if adjustedNext != nil {
		base = adjustedNext.Add(-time.Duration(minMinutes) * time.Minute)
	}
	slots := make([]Slot, 0, count)
	for i := 1; i <= count; i++ {
		s := Slot{
			Index: i,
			Start: base.Add(time.Duration(i*minMinutes) * time.Minute),
			End:   base.Add(time.Duration((i-1)*minMinutes+maxMinutes) * time.Minute),
		}
		if m, ok := asn[i]; ok {
			s.StatusA = m["a"]
			s.StatusB = m["b"]
		}
		slots = append(slots, s)
	}
	return slots
}

// FormatSlotRange returns either "HH:MM" (when min==max) or
// "HH:MM - HH:MM", or with a date prefix "M/D HH:MM" when the slot spans into
// another day relative to nowInLoc.
func FormatSlotRange(s Slot, loc *time.Location, nowInLoc time.Time) string {
	start := s.Start.In(loc)
	end := s.End.In(loc)
	startStr := formatPoint(start, nowInLoc)
	if start.Equal(end) {
		return startStr
	}
	endStr := formatPoint(end, nowInLoc)
	return fmt.Sprintf("%s - %s", startStr, endStr)
}

func formatPoint(t time.Time, ref time.Time) string {
	if sameDay(t, ref) {
		return t.Format("15:04")
	}
	return fmt.Sprintf("%d/%d %s", int(t.Month()), t.Day(), t.Format("15:04"))
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
