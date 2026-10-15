package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/paulhmurray/tiller/internal/vault"
)

// Goals: a list screen, a detail screen with milestones, journal mentions
// and reviews, a guided "new goal" chain of prompts, and a weekly walk
// that asks for a one-line review per active goal.

const quietAfter = 14 * 24 * time.Hour

type goalDraft struct {
	title, why, horizon string
	keywords            []string
}

type goalWalk struct {
	queue []string // goal ids
	pos   int
	done  int
}

func (m *Model) loadGoals() {
	goals, err := m.v.Goals()
	if err != nil {
		m.err = err
	}
	vault.Mentions(goals, m.days)
	m.goals = goals
	if m.goalID != "" {
		m.goal = m.findGoal(m.goalID)
	}
}

func (m *Model) findGoal(id string) *vault.Goal {
	for i := range m.goals {
		if m.goals[i].ID == id {
			return &m.goals[i]
		}
	}
	return nil
}

func (m *Model) activeGoals() []*vault.Goal {
	var out []*vault.Goal
	for i := range m.goals {
		if m.goals[i].Active() {
			out = append(out, &m.goals[i])
		}
	}
	return out
}

func (m *Model) goalItem(section string, g *vault.Goal) item {
	done, total := g.Progress()
	it := item{section: section, kind: rowGoal, key: g.ID, label: g.Title, tags: g.Areas, degree: done, words: total, path: m.v.GoalsPath(), line: g.Line}
	it.age = "never"
	if t, ok := g.LastTouched(); ok {
		it.age = ago(t, m.now)
		if m.now.Sub(t) > quietAfter {
			it.missing = "quiet" // rendered amber
		}
	} else {
		it.missing = "quiet"
	}
	it.horizon = g.Horizon
	if !g.Active() {
		it.horizon = strings.ToLower(g.State)
	}
	return it
}

func (m *Model) buildGoalItems() {
	switch m.screen {
	case screenGoals:
		for _, g := range m.activeGoals() {
			m.items = append(m.items, m.goalItem("active goals", g))
		}
		for i := range m.goals {
			if !m.goals[i].Active() {
				m.items = append(m.items, m.goalItem("finished", &m.goals[i]))
			}
		}
	case screenGoal:
		g := m.goal
		for _, ms := range g.Milestones {
			m.items = append(m.items, item{section: "milestones", kind: rowMilestone, label: ms.Title, done: ms.Done, line: ms.Line, path: m.v.GoalsPath()})
		}
		// Last five journal mentions, newest first.
		shown := 0
		for i := len(g.Touched) - 1; i >= 0 && shown < 5; i-- {
			for _, d := range m.days {
				if d.Date.Equal(g.Touched[i]) {
					first := ""
					if len(d.Entries) > 0 {
						first = d.Entries[0].Title
					}
					m.items = append(m.items, m.dayItem("journal mentions", d.Date, d.Path, first, d.Words))
					shown++
				}
			}
		}
		for i := len(g.Reviews) - 1; i >= 0; i-- {
			r := g.Reviews[i]
			m.items = append(m.items, item{section: "reviews", kind: rowReview, label: r.Date.Format("Mon 02 Jan"), tags: []string{r.Text}, line: r.Line, path: m.v.GoalsPath()})
		}
	}
}

func (m *Model) openGoals() {
	m.push()
	m.screen, m.cursor, m.note = screenGoals, 0, nil
	m.buildItems()
}

func (m *Model) openGoal(id string) {
	g := m.findGoal(id)
	if g == nil {
		return
	}
	m.push()
	m.screen, m.cursor, m.goal, m.goalID = screenGoal, 0, g, id
	m.buildItems()
}

// --- new goal: title, why, horizon, keywords ---

func (m *Model) startNewGoal() tea.Cmd {
	m.draft = goalDraft{}
	m.mode = modeGoalTitle
	m.input.Placeholder = "what do you want to be true?"
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m Model) updateNewGoal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kEsc, "ctrl+c":
		// Once there is a title the goal is worth keeping: save what we
		// have rather than losing it. Before a title there is nothing.
		m.mode = modeBrowse
		m.input.Blur()
		if m.draft.title == "" {
			m.status = "cancelled, nothing saved"
			return m, nil
		}
		return m.saveDraft("saved early; e opens goals.org to fill in the rest")
	case kEnter:
		val := strings.TrimSpace(m.input.Value())
		switch m.mode {
		case modeGoalTitle:
			if val == "" {
				return m, nil
			}
			m.draft.title = val
			m.mode = modeGoalWhy
			m.input.Placeholder = "why does it matter? (enter to skip)"
		case modeGoalWhy:
			m.draft.why = val
			m.mode = modeGoalHorizon
			m.input.Placeholder = "by when? e.g. 2027-04 (enter to skip)"
		case modeGoalHorizon:
			m.draft.horizon = val
			m.mode = modeGoalKeywords
			m.input.Placeholder = "words you'd write in the journal when you worked on it, e.g. outline, chapter (enter to skip)"
		case modeGoalKeywords:
			for _, k := range strings.Split(val, ",") {
				if k = strings.TrimSpace(k); k != "" {
					m.draft.keywords = append(m.draft.keywords, k)
				}
			}
			m.mode = modeBrowse
			m.input.Blur()
			return m.saveDraft("added; enter opens it, a adds a milestone")
		}
		m.input.SetValue("")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) saveDraft(note string) (tea.Model, tea.Cmd) {
	line, err := m.v.AddGoal(m.draft.title, "", m.draft.horizon, m.draft.why, m.draft.keywords, time.Now())
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.reload()
	m.status = m.draft.title + ": " + note
	if m.screen == screenGoals {
		for i, it := range m.items {
			if it.line == line {
				m.cursor = i
			}
		}
	}
	return m, nil
}

// --- milestone and review prompts on the goal screen ---

func (m *Model) startMilestone() tea.Cmd {
	m.mode = modeMilestone
	m.input.Placeholder = "a concrete step you can finish"
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m *Model) startReview() tea.Cmd {
	m.mode = modeReview
	m.input.Placeholder = "what moved, and what's next?"
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m Model) updateGoalInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case kEsc, "ctrl+c":
		m.mode = modeBrowse
		m.input.Blur()
		if m.screen == screenGoalWalk {
			m.walkAdvance()
		}
		return m, nil
	case kEnter:
		val := strings.TrimSpace(m.input.Value())
		mode := m.mode
		m.mode = modeBrowse
		m.input.Blur()
		if val == "" {
			if m.screen == screenGoalWalk {
				m.walkAdvance()
			}
			return m, nil
		}
		var err error
		if mode == modeMilestone {
			err = m.v.AddMilestone(*m.goal, val)
		} else {
			err = m.v.AddReview(*m.goal, val, time.Now())
		}
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.reload()
		if m.screen == screenGoalWalk {
			m.walk.done++
			m.walkAdvance()
			return m, nil
		}
		m.status = "saved"
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// --- the weekly walk ---

func (m *Model) startWalk() tea.Cmd {
	var ids []string
	for _, g := range m.activeGoals() {
		ids = append(ids, g.ID)
	}
	if len(ids) == 0 {
		m.status = "no active goals to review"
		return nil
	}
	m.push()
	m.screen = screenGoalWalk
	m.walk = goalWalk{queue: ids}
	return m.walkSync()
}

func (m *Model) walkSync() tea.Cmd {
	for m.walk.pos < len(m.walk.queue) {
		if g := m.findGoal(m.walk.queue[m.walk.pos]); g != nil {
			m.goal, m.goalID = g, g.ID
			m.cursor = 0
			m.buildItems()
			return m.startReview()
		}
		m.walk.pos++
	}
	m.back()
	m.status = fmt.Sprintf("review done: %d of %d goals updated", m.walk.done, len(m.walk.queue))
	return nil
}

func (m *Model) walkAdvance() tea.Cmd {
	m.walk.pos++
	return m.walkSync()
}

func (m Model) viewGoalsHeader(b *strings.Builder) {
	b.WriteString(sDim.Render("outcomes you're working toward; quiet means the journal hasn't mentioned it in two weeks") + "\n")
}

func (m Model) viewGoalHeader(b *strings.Builder) {
	g := m.goal
	if m.screen == screenGoalWalk {
		b.WriteString(sDim.Render(fmt.Sprintf("weekly review, goal %d of %d", m.walk.pos+1, len(m.walk.queue))) + "\n\n")
	}
	state := g.State
	if state == "" {
		state = "open"
	}
	b.WriteString(sLabel.Render("state") + sValue.Render(strings.ToLower(state)) + "\n")
	if g.Why != "" {
		b.WriteString(sLabel.Render("why") + sValue.Render(g.Why) + "\n")
	}
	if g.Horizon != "" {
		b.WriteString(sLabel.Render("by") + sValue.Render(g.Horizon) + "\n")
	}
	if len(g.Areas) > 0 {
		b.WriteString(sLabel.Render("area") + sTag.Render(":"+strings.Join(g.Areas, ":")+":") + "\n")
	}
	if len(g.Keywords) > 0 {
		b.WriteString(sLabel.Render("keywords") + sDim.Render(strings.Join(g.Keywords, ", ")) + "\n")
	}
	done, total := g.Progress()
	prog := sDim.Render("no milestones yet; a adds one")
	if total > 0 {
		prog = sValue.Render(fmt.Sprintf("%d of %d milestones", done, total)) + "  " + bar(done, total, 20)
	}
	b.WriteString(sLabel.Render("progress") + prog + "\n")
	touched := sWarn.Render("never mentioned in the journal")
	if t, ok := g.LastTouched(); ok {
		touched = sValue.Render(fmt.Sprintf("%s, %s in all", ago(t, m.now), plural(len(g.Touched), "mention", "mentions")))
		if m.now.Sub(t) > quietAfter {
			touched += "  " + sWarn.Render("quiet")
		}
	}
	b.WriteString(sLabel.Render("journal") + touched + "\n")
}

func bar(done, total, width int) string {
	if total == 0 {
		return ""
	}
	filled := done * width / total
	return sCursor.Render(strings.Repeat("█", filled)) + sDim.Render(strings.Repeat("░", width-filled))
}
