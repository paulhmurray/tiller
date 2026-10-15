package tui

import "strings"

// The guide is the four loops in plain prose, on H. Tips are the one-line
// version, one per screen visit, shown above the keys.

var guide = []struct{ head, body string }{
	{"journal", "J asks for a headline, then opens today's file in nvim. One file per day, so both machines can write without colliding. Write about the goals and the book by name: that is how tiller notices you touched them."},
	{"notes", "Every note is born fleeting. Processing it means rewriting it in your own words, linking it to at least one other note, then promoting it with p. f lists what is waiting, oldest first; P walks them one at a time with suggestions. An orphan has no links in or out; y copies a link to paste elsewhere, l and L write links without leaving tiller."},
	{"goals", "A goal is an outcome with a why, a horizon and milestones. Keywords are the words you would write in the journal on a day you worked on it; tiller counts those days as touching the goal and says so when it has gone quiet. P on the goals list is the weekly review: one dated line per goal."},
	{"books", "The zettelkasten holds the ideas behind a book: characters, places, themes, research. The outline holds the structure, one heading per chapter with a promise and links to the notes it uses. The prose lives in one file per chapter. Write in the chapter file; drop stray ideas in as fleeting notes rather than derailing the prose."},
	{"keys", "? hides and shows the key panel on every screen. Each screen also shows one tip at a time; they change as you move around."},
}

func (m Model) viewGuide(b *strings.Builder) {
	width := m.width
	if width <= 0 {
		width = 100
	}
	for _, g := range guide {
		b.WriteString(sHeader.Render(g.head) + "\n")
		for _, l := range wrap(g.body, min(96, width-6)) {
			b.WriteString("  " + sValue.Render(l) + "\n")
		}
	}
}

// Tips per screen. Every key named here must exist in that screen's table;
// the keys test checks it.
var tips = map[screen][]string{
	screenHome: {
		"J to write: it asks for a headline first, a word or a place is plenty",
		"f shows fleeting notes waiting; P walks them one at a time",
		"G for goals, B for the book; both read your journal for mentions",
		"H explains how the pieces fit together",
		"n makes a note; it starts fleeting on purpose, so you come back to it",
	},
	screenNotes: {
		"/ filters by title or tag as you type; esc clears it",
		"tab cycles all, fleeting, orphans, missing",
		"enter on a missing note creates it, already wanted by the note that linked it",
	},
	screenNote: {
		"l links this note to another; it is written under * Related, so nvim follows it too",
		"L does the reverse: the note you pick gets a link to this one",
		"p promotes, but only once the note is linked: an unlinked permanent note is just an orphan",
		"N makes a new note that already links back here",
		"y copies [[id:…][title]] for pasting in nvim with \"+p",
		"t edits the tags in place",
	},
	screenProcess: {
		"1 to 5 link a suggestion; l searches everything",
		"s skips a note for now; it stays fleeting and comes round next time",
		"e rewrites in nvim; come back and p when it is yours and linked",
	},
	screenGoals: {
		"n walks four prompts: the goal, why, by when, and the journal words that mean you touched it",
		"P is the weekly review: one dated line per goal, enter with nothing skips",
		"quiet means the journal has not mentioned it in two weeks",
	},
	screenGoal: {
		"a adds a milestone; x or enter ticks it",
		"R writes a dated review line under the goal",
		"e opens goals.org at this goal; the keywords live in its property drawer",
	},
	screenBooks: {
		"n starts a book: a title and a word target",
		"enter opens a book; its outline and chapters live under book/",
	},
	screenBook: {
		"enter opens the chapter's prose file in nvim",
		"o opens the outline at this chapter: its promise and the notes it uses",
		"s cycles the stage: outline, draft, revise, done",
		"a adds a chapter at the end and makes its file",
		"today's words count from the first time tiller looks each day",
	},
	screenGuide: {
		"h goes back",
	},
}

func (m Model) tip() string {
	ts := tips[m.screen]
	if len(ts) == 0 {
		return ""
	}
	return ts[(m.tipSeed+m.now.YearDay())%len(ts)]
}
