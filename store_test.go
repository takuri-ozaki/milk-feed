package main

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func setAssignments(t *testing.T, st *Store, entries map[int]map[string]string) {
	t.Helper()
	for idx, m := range entries {
		for cg, status := range m {
			if err := st.SetAssignment(idx, cg, status); err != nil {
				t.Fatalf("SetAssignment(%d, %s, %s): %v", idx, cg, status, err)
			}
		}
	}
}

func dumpAssignments(t *testing.T, st *Store) AssignmentMap {
	t.Helper()
	a, err := st.AllAssignments()
	if err != nil {
		t.Fatalf("AllAssignments: %v", err)
	}
	return a
}

func assertAssignments(t *testing.T, got AssignmentMap, want map[int]map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("assignment slot count: got=%d want=%d (got=%v)", len(got), len(want), got)
	}
	for idx, m := range want {
		gm, ok := got[idx]
		if !ok {
			t.Fatalf("missing slot %d in got", idx)
		}
		if len(gm) != len(m) {
			t.Fatalf("slot %d size: got=%d want=%d", idx, len(gm), len(m))
		}
		for cg, status := range m {
			if gm[cg] != status {
				t.Fatalf("slot %d %s: got=%q want=%q", idx, cg, gm[cg], status)
			}
		}
	}
}

func TestDeleteLatestFeeding_ShiftsAssignmentsUp(t *testing.T) {
	st := newTestStore(t)

	t1 := time.Date(2026, 5, 7, 3, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC)

	if err := st.AddFeedingAndShift(t1); err != nil {
		t.Fatal(err)
	}
	if err := st.AddFeedingAndShift(t2); err != nil {
		t.Fatal(err)
	}

	// Caregiver plans the next 3 slots after t2.
	setAssignments(t, st, map[int]map[string]string{
		1: {"a": "o"},
		2: {"a": "x", "b": "t"},
		3: {"a": "o"},
	})

	feeds, err := st.RecentFeedings(10)
	if err != nil {
		t.Fatal(err)
	}
	if feeds[0].FedAt.Unix() != t2.Unix() {
		t.Fatalf("latest is not t2")
	}

	// Delete the latest feeding (t2).
	if err := st.DeleteFeeding(feeds[0].ID); err != nil {
		t.Fatal(err)
	}

	// Anchor reverts to t1; assignments should shift up so slot 1 is empty
	// (unrecoverable) and the prior slots N are now at N+1.
	got := dumpAssignments(t, st)
	assertAssignments(t, got, map[int]map[string]string{
		2: {"a": "o"},
		3: {"a": "x", "b": "t"},
		4: {"a": "o"},
	})

	latest, has, err := st.LatestFeedingTime()
	if err != nil {
		t.Fatal(err)
	}
	if !has || latest.Unix() != t1.Unix() {
		t.Fatalf("latest after delete: got=%v want=%v", latest, t1)
	}
}

func TestDeleteNonLatestFeeding_DoesNotShift(t *testing.T) {
	st := newTestStore(t)

	t1 := time.Date(2026, 5, 7, 3, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC)

	if err := st.AddFeedingAndShift(t1); err != nil {
		t.Fatal(err)
	}
	if err := st.AddFeedingAndShift(t2); err != nil {
		t.Fatal(err)
	}

	setAssignments(t, st, map[int]map[string]string{
		1: {"a": "o"},
		2: {"b": "x"},
	})

	feeds, err := st.RecentFeedings(10)
	if err != nil {
		t.Fatal(err)
	}
	// feeds[0] is t2 (latest), feeds[1] is t1 (older).
	older := feeds[1]
	if older.FedAt.Unix() != t1.Unix() {
		t.Fatalf("expected feeds[1] to be t1")
	}

	if err := st.DeleteFeeding(older.ID); err != nil {
		t.Fatal(err)
	}

	// Anchor unchanged → assignments unchanged.
	got := dumpAssignments(t, st)
	assertAssignments(t, got, map[int]map[string]string{
		1: {"a": "o"},
		2: {"b": "x"},
	})

	latest, has, err := st.LatestFeedingTime()
	if err != nil {
		t.Fatal(err)
	}
	if !has || latest.Unix() != t2.Unix() {
		t.Fatalf("latest should remain t2; got=%v", latest)
	}
}

func TestAddFeedingAndShift_ShiftsAssignmentsDown(t *testing.T) {
	st := newTestStore(t)

	if err := st.AddFeedingAndShift(time.Date(2026, 5, 7, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	setAssignments(t, st, map[int]map[string]string{
		1: {"a": "o"},
		2: {"a": "x", "b": "t"},
		3: {"a": "o"},
	})

	if err := st.AddFeedingAndShift(time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	// Slot 1 was consumed; old slot N (>1) is now slot N-1.
	got := dumpAssignments(t, st)
	assertAssignments(t, got, map[int]map[string]string{
		1: {"a": "x", "b": "t"},
		2: {"a": "o"},
	})
}

func TestRecordThenDelete_RestoresAssignmentsExceptSlot1(t *testing.T) {
	st := newTestStore(t)

	if err := st.AddFeedingAndShift(time.Date(2026, 5, 7, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	setAssignments(t, st, map[int]map[string]string{
		1: {"a": "o"},
		2: {"a": "x"},
		3: {"a": "t"},
	})

	if err := st.AddFeedingAndShift(time.Date(2026, 5, 7, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	feeds, err := st.RecentFeedings(10)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteFeeding(feeds[0].ID); err != nil {
		t.Fatal(err)
	}

	// Slot 1 (originally "o") is unrecoverable. Slots 2,3 are restored.
	got := dumpAssignments(t, st)
	assertAssignments(t, got, map[int]map[string]string{
		2: {"a": "x"},
		3: {"a": "t"},
	})
}

func TestDeleteOnlyFeeding_ShiftsUp(t *testing.T) {
	st := newTestStore(t)

	t1 := time.Date(2026, 5, 7, 3, 0, 0, 0, time.UTC)
	if err := st.AddFeedingAndShift(t1); err != nil {
		t.Fatal(err)
	}
	setAssignments(t, st, map[int]map[string]string{
		1: {"a": "o"},
		2: {"a": "t"},
	})
	feeds, err := st.RecentFeedings(10)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteFeeding(feeds[0].ID); err != nil {
		t.Fatal(err)
	}
	got := dumpAssignments(t, st)
	assertAssignments(t, got, map[int]map[string]string{
		2: {"a": "o"},
		3: {"a": "t"},
	})
	if _, has, err := st.LatestFeedingTime(); err != nil || has {
		t.Fatalf("expected no feeding remaining (has=%v err=%v)", has, err)
	}
}
