package tui

import "testing"

// Every table must be free of duplicate keys, or the panel would lie.
func TestKeyTablesUnique(t *testing.T) {
	tables := map[string][]binding{"home": keysHome, "notes": keysNotes, "note": keysNote, "process": keysProcess,
		"picker": keysPicker, "goals": keysGoals, "goal": keysGoal, "newkind": keysNewKind, "input": keysInput, "confirm": keysConfirm}
	for name, tb := range tables {
		seen := map[string]bool{}
		for _, b := range tb {
			if seen[b.key] {
				t.Errorf("%s: duplicate key %q", name, b.key)
			}
			seen[b.key] = true
			if b.desc == "" {
				t.Errorf("%s: key %q has no description", name, b.key)
			}
		}
	}
}
