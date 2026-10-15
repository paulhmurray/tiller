package tui

import (
	"strings"
	"testing"
)

// Every table must be free of duplicate keys, or the panel would lie.
func TestKeyTablesUnique(t *testing.T) {
	tables := map[string][]binding{"home": keysHome, "notes": keysNotes, "note": keysNote, "process": keysProcess,
		"picker": keysPicker, "goals": keysGoals, "goal": keysGoal, "books": keysBooks, "book": keysBook, "guide": keysGuide, "newkind": keysNewKind, "input": keysInput, "confirm": keysConfirm}
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

// Every key a tip names must exist on that screen, so tips never lie.
func TestTipsNameRealKeys(t *testing.T) {
	tables := map[screen][]binding{screenHome: keysHome, screenNotes: keysNotes, screenNote: keysNote, screenProcess: keysProcess,
		screenGoals: keysGoals, screenGoal: keysGoal, screenBooks: keysBooks, screenBook: keysBook, screenGuide: keysGuide}
	for sc, ts := range tips {
		have := map[string]bool{}
		for _, b := range tables[sc] {
			have[b.key] = true
			for _, k := range strings.Split(b.key, "/") {
				have[strings.TrimSpace(k)] = true
			}
		}
		have["1"], have["5"] = have["1-5"], have["1-5"]
		for _, tip := range ts {
			first := strings.Fields(tip)[0]
			if len(first) == 1 && !have[first] {
				t.Errorf("screen %d tip names key %q which the screen lacks: %q", sc, first, tip)
			}
		}
	}
}
