package vault

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestGoals(t *testing.T) {
	v := Vault{Root: t.TempDir()}
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.Local)
	line, err := v.AddGoal("Run a half marathon", "health", "2027-04", "to feel strong at 50", []string{"run", "long run"}, now)
	if err != nil || line != 3 {
		t.Fatal(err, line)
	}
	if _, err := v.AddGoal("Ship tiller", "craft", "", "", nil, now); err != nil {
		t.Fatal(err)
	}
	goals, err := v.Goals()
	if err != nil || len(goals) != 2 {
		t.Fatalf("%v %d", err, len(goals))
	}
	g := goals[0]
	if g.Title != "Run a half marathon" || g.State != "TODO" || g.ID != "goal-run-a-half-marathon" || g.Horizon != "2027-04" || g.Why != "to feel strong at 50" || len(g.Keywords) != 2 || g.Areas[0] != "health" {
		t.Errorf("parsed goal: %+v", g)
	}
	if err := v.AddMilestone(g, "Run 10k"); err != nil {
		t.Fatal(err)
	}
	goals, _ = v.Goals()
	if err := v.AddMilestone(goals[0], "Enter the race"); err != nil {
		t.Fatal(err)
	}
	goals, _ = v.Goals()
	g = goals[0]
	if len(g.Milestones) != 2 || g.Milestones[0].Title != "Run 10k" || g.Milestones[1].Title != "Enter the race" {
		t.Fatalf("milestones: %+v", g.Milestones)
	}
	if err := v.ToggleMilestone(g.Milestones[0].Line); err != nil {
		t.Fatal(err)
	}
	if err := v.AddReview(g, "Moved: two runs. Next: a 12k.", now); err != nil {
		t.Fatal(err)
	}
	goals, _ = v.Goals()
	g = goals[0]
	d, total := g.Progress()
	if d != 1 || total != 2 || len(g.Reviews) != 1 || !strings.HasPrefix(g.Reviews[0].Text, "Moved") {
		t.Errorf("after toggle+review: %+v", g)
	}
	// Second goal untouched by edits to the first.
	if goals[1].Title != "Ship tiller" || len(goals[1].Milestones) != 0 {
		t.Errorf("second goal disturbed: %+v", goals[1])
	}
	b, _ := os.ReadFile(v.GoalsPath())
	want := "** DONE Run 10k\n** TODO Enter the race\n** Reviews\n*** [2026-10-15 Thu] Moved: two runs. Next: a 12k.\n\n* TODO Ship tiller  :craft:\n"
	if !strings.Contains(string(b), want) {
		t.Errorf("file layout:\n%s", b)
	}
	// Mentions by keyword, whole word, case-insensitive.
	days := []Day{
		{Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), Entries: []Entry{{Title: "Morning", Body: []string{"Went for a RUN before work."}}}},
		{Date: time.Date(2026, 10, 2, 0, 0, 0, 0, time.Local), Entries: []Entry{{Title: "Runway", Body: []string{"airport day"}}}},
		{Date: time.Date(2026, 10, 3, 0, 0, 0, 0, time.Local), Entries: []Entry{{Title: "Tiller", Body: []string{"built the goals screen"}}}},
	}
	Mentions(goals, days)
	if len(goals[0].Touched) != 1 || goals[0].Touched[0].Day() != 1 {
		t.Errorf("mentions for goal 0: %v", goals[0].Touched)
	}
	if len(goals[1].Touched) != 1 || goals[1].Touched[0].Day() != 3 {
		t.Errorf("title-word fallback for goal 1: %v", goals[1].Touched)
	}
}
