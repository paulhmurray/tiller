package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/org"
)

// Goals live in <root>/goals.org, one level-1 headline per goal:
//
//	* TODO Run a half marathon                              :health:
//	  :PROPERTIES:
//	  :ID: goal-half-marathon
//	  :HORIZON: 2027-04
//	  :KEYWORDS: run, running, long run
//	  :END:
//	  Why: because ...
//	** TODO Run 10k without stopping          <- milestones: level-2 TODO/DONE
//	** DONE Buy shoes
//	** Reviews
//	*** [2026-10-19 Sun] Moved: ... Next: ...
//
// The TODO keyword set is the one nvim-orgmode is configured with, so the
// file also shows up in the org agenda unchanged.

const GoalsFile = "goals.org"

var (
	todoRe = regexp.MustCompile(`^(TODO|NEXT|WAITING|DONE|CANCELLED)\s+(.*)$`)
)

type Milestone struct {
	Title string
	Done  bool
	State string
	Line  int
}

type Review struct {
	Date time.Time
	Text string
	Line int
}

type Goal struct {
	Title      string
	ID         string
	State      string // TODO, NEXT, WAITING, DONE, CANCELLED, or "" when no keyword
	Areas      []string
	Horizon    string // free text, usually YYYY-MM
	Keywords   []string
	Why        string
	Line       int // headline line in goals.org, for jumping nvim there
	End        int
	Milestones []Milestone
	Reviews    []Review
	// Filled by Mentions: journal days whose text matched a keyword.
	Touched []time.Time
}

func (g Goal) Active() bool { return g.State != "DONE" && g.State != "CANCELLED" }

func (g Goal) Progress() (done, total int) {
	for _, m := range g.Milestones {
		total++
		if m.Done {
			done++
		}
	}
	return
}

func (g Goal) LastTouched() (time.Time, bool) {
	if len(g.Touched) == 0 {
		return time.Time{}, false
	}
	return g.Touched[len(g.Touched)-1], true
}

func (v Vault) GoalsPath() string { return filepath.Join(v.Root, GoalsFile) }

// Goals parses goals.org. A missing file is an empty list, not an error.
func (v Vault) Goals() ([]Goal, error) {
	doc, err := org.ParseFile(v.GoalsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var goals []Goal
	for i, h := range doc.Headlines {
		if h.Level != 1 {
			continue
		}
		g := Goal{Line: h.Line, End: h.End, Areas: h.Tags, ID: h.Props["ID"], Horizon: h.Props["HORIZON"]}
		g.State, g.Title = splitState(h.Title)
		for _, k := range strings.Split(h.Props["KEYWORDS"], ",") {
			if k = strings.TrimSpace(k); k != "" {
				g.Keywords = append(g.Keywords, k)
			}
		}
		for _, l := range doc.Lines[h.ContentStart:h.End] {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "*") {
				break
			}
			g.Why = strings.TrimSpace(strings.TrimPrefix(t, "Why:"))
			break
		}
		for _, sub := range doc.Headlines[i+1:] {
			if sub.Line >= h.End {
				break
			}
			switch {
			case sub.Level == 2 && strings.EqualFold(sub.Title, "Reviews"):
			case sub.Level == 2:
				st, title := splitState(sub.Title)
				if st == "" {
					continue // a plain sub-heading, not a milestone
				}
				g.Milestones = append(g.Milestones, Milestone{Title: title, Done: st == "DONE" || st == "CANCELLED", State: st, Line: sub.Line})
			case sub.Level == 3:
				text, when := org.StripStamp(sub.Title)
				if !when.IsZero() {
					g.Reviews = append(g.Reviews, Review{Date: when, Text: text, Line: sub.Line})
				}
			}
		}
		goals = append(goals, g)
	}
	return goals, nil
}

func splitState(title string) (state, rest string) {
	if m := todoRe.FindStringSubmatch(title); m != nil {
		return m[1], strings.TrimSpace(m[2])
	}
	return "", title
}

// Mentions fills Touched for every goal by matching keywords as whole
// words, case-insensitively, against journal entries. Goals with no
// keywords use their title words of four letters or more.
func Mentions(goals []Goal, days []Day) {
	for gi := range goals {
		g := &goals[gi]
		words := g.Keywords
		if len(words) == 0 {
			for _, w := range strings.Fields(g.Title) {
				if len(w) >= 4 {
					words = append(words, w)
				}
			}
		}
		if len(words) == 0 {
			continue
		}
		var pats []*regexp.Regexp
		for _, w := range words {
			pats = append(pats, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(w)+`\b`))
		}
		g.Touched = g.Touched[:0]
		for _, d := range days {
			text := dayText(d)
			for _, p := range pats {
				if p.MatchString(text) {
					g.Touched = append(g.Touched, d.Date)
					break
				}
			}
		}
		sort.Slice(g.Touched, func(i, j int) bool { return g.Touched[i].Before(g.Touched[j]) })
	}
}

func dayText(d Day) string {
	var sb strings.Builder
	for _, e := range d.Entries {
		sb.WriteString(e.Title + "\n" + strings.Join(e.Body, "\n") + "\n")
	}
	return sb.String()
}

// --- writes: each one edits goals.org in place and preserves everything else ---

func (v Vault) readGoalLines() ([]string, error) {
	doc, err := org.ParseFile(v.GoalsPath())
	if err != nil {
		return nil, err
	}
	return doc.Lines, nil
}

func (v Vault) writeGoalLines(lines []string) error {
	return os.WriteFile(v.GoalsPath(), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// AddGoal appends a goal and returns its headline line number.
func (v Vault) AddGoal(title, area, horizon, why string, keywords []string, now time.Time) (int, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, fmt.Errorf("empty title")
	}
	var lines []string
	if _, err := os.Stat(v.GoalsPath()); err == nil {
		lines, err = v.readGoalLines()
		if err != nil {
			return 0, err
		}
	} else {
		lines = []string{"#+TITLE: Goals", "#+STARTUP: content", ""}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	head := "* TODO " + title
	if area != "" {
		head += "  :" + strings.Trim(area, ":") + ":"
	}
	at := len(lines)
	lines = append(lines, head,
		"  :PROPERTIES:",
		"  :ID: goal-"+org.Slugify(title),
		"  :CREATED: "+org.Stamp(now))
	if horizon != "" {
		lines = append(lines, "  :HORIZON: "+horizon)
	}
	if len(keywords) > 0 {
		lines = append(lines, "  :KEYWORDS: "+strings.Join(keywords, ", "))
	}
	lines = append(lines, "  :END:")
	if why != "" {
		lines = append(lines, "  Why: "+why)
	}
	return at, v.writeGoalLines(lines)
}

// ToggleMilestone flips TODO <-> DONE on the milestone's headline line.
func (v Vault) ToggleMilestone(line int) error {
	lines, err := v.readGoalLines()
	if err != nil {
		return err
	}
	if line < 0 || line >= len(lines) {
		return fmt.Errorf("line out of range")
	}
	l := lines[line]
	switch {
	case strings.HasPrefix(l, "** DONE "):
		lines[line] = "** TODO " + strings.TrimPrefix(l, "** DONE ")
	case strings.HasPrefix(l, "** TODO ") || strings.HasPrefix(l, "** NEXT ") || strings.HasPrefix(l, "** WAITING "):
		_, rest := splitState(strings.TrimPrefix(l, "** "))
		lines[line] = "** DONE " + rest
	default:
		return fmt.Errorf("not a milestone line")
	}
	return v.writeGoalLines(lines)
}

// AddMilestone inserts "** TODO title" after the goal's last milestone,
// or right after its properties and why when it has none.
func (v Vault) AddMilestone(g Goal, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("empty milestone")
	}
	lines, err := v.readGoalLines()
	if err != nil {
		return err
	}
	at := g.milestoneInsertAt(lines)
	lines = append(lines[:at], append([]string{"** TODO " + title}, lines[at:]...)...)
	return v.writeGoalLines(lines)
}

func (g Goal) milestoneInsertAt(lines []string) int {
	if n := len(g.Milestones); n > 0 {
		return g.Milestones[n-1].Line + 1
	}
	// After the goal's own body: first level-2 heading, else the goal end.
	for i := g.Line + 1; i < g.End && i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "** ") {
			return i
		}
	}
	at := g.End
	for at > g.Line+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	return at
}

// AddReview appends a dated entry under the goal's Reviews heading,
// creating the heading at the end of the goal when needed.
func (v Vault) AddReview(g Goal, text string, now time.Time) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty review")
	}
	lines, err := v.readGoalLines()
	if err != nil {
		return err
	}
	entry := "*** " + now.Format("[2006-01-02 Mon]") + " " + text
	at := -1
	for i := g.Line + 1; i < g.End && i < len(lines); i++ {
		if strings.EqualFold(strings.TrimSpace(lines[i]), "** Reviews") {
			at = g.End
			for at > i+1 && strings.TrimSpace(lines[at-1]) == "" {
				at--
			}
			break
		}
	}
	var ins []string
	if at < 0 {
		at = g.End
		for at > g.Line+1 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}
		ins = []string{"** Reviews", entry}
	} else {
		ins = []string{entry}
	}
	lines = append(lines[:at], append(ins, lines[at:]...)...)
	return v.writeGoalLines(lines)
}
