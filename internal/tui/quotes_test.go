package tui

import (
	"testing"
	"time"
)

func TestQuotes(t *testing.T) {
	seen := map[string]bool{}
	for _, q := range quotes {
		if q.text == "" || q.who == "" {
			t.Errorf("incomplete quote: %+v", q)
		}
		if seen[q.text] {
			t.Errorf("duplicate quote: %q", q.text)
		}
		seen[q.text] = true
	}
	a, _ := QuoteFor(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	b, _ := QuoteFor(time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC))
	c, _ := QuoteFor(time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC))
	if a != b || a == c {
		t.Errorf("quote should be stable within a day and change across days")
	}
	if got := wrap("one two three four five", 9); len(got) != 3 || got[0] != "one two" {
		t.Errorf("wrap = %q", got)
	}
}

func TestWrapBalanced(t *testing.T) {
	got := wrapBalanced("Begin at once to live, and count each separate day as a separate life.", 64)
	if len(got) != 2 || got[0] != "Begin at once to live, and count each" || got[1] != "separate day as a separate life." {
		t.Errorf("balanced = %q", got)
	}
}
