// Package migrate converts the pre-tiller layout (one journal.org plus a
// zettelkasten/ tree with inbox files and type folders) into the vault
// layout. It is a one-shot: once journal.org and zettelkasten/ are gone
// it has nothing to do.
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/org"
	"github.com/paulhmurray/tiller/internal/vault"
)

type Plan struct {
	Root     string
	BakDir   string
	Writes   map[string]string // path -> content
	Moves    [][2]string       // from -> to (into BakDir)
	Warnings []string
	// notes built during planning, for link resolution
	notes []*note
	// origin basename (e.g. "agile-scrum.org") -> note id
	byFile map[string]string
	// origin basename + "::" + headline title (raw and stripped) -> id
	byHeadline map[string]string
}

type note struct {
	id, slug, title string
	tags            []string
	created         string        // raw org timestamp, e.g. [2026-01-24 Fri] or [2026-01-28 Wed 08:07]
	keywords        []org.Keyword // extra keywords to preserve (AUTHOR, SOURCE, STARTUP...)
	body            []string
	srcFile         string // basename of the originating file
	srcHeadline     string // raw headline title, when split from an inbox
}

var (
	skipFiles = map[string]bool{"index.org": true, "WORKFLOW.org": true, "UPDATED-WORKFLOW.org": true}
	stemStamp = regexp.MustCompile(`^(\d{8})(\d{6})?[-_]?`)
	slugLike  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	uuidLike  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	dateHead  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\b`)
)

func Build(root string, now time.Time) (*Plan, error) {
	p := &Plan{
		Root:       root,
		BakDir:     filepath.Join(root, ".tiller-bak", now.Format("20060102-150405")),
		Writes:     map[string]string{},
		byFile:     map[string]string{},
		byHeadline: map[string]string{},
	}
	if err := p.planJournal(); err != nil {
		return nil, err
	}
	if err := p.planNotes(); err != nil {
		return nil, err
	}
	p.resolveLinks()
	p.renderNotes()
	if ids := filepath.Join(root, ".orgids"); exists(ids) && (len(p.Moves) > 0) {
		p.Moves = append(p.Moves, [2]string{ids, filepath.Join(p.BakDir, ".orgids")})
	}
	return p, nil
}

// ---------- journal ----------

type dayAcc struct {
	title   string
	entries []vault.Entry
	seen    map[string]bool
	merged  bool // started from an existing day file
	changed bool // gained at least one entry from a source file
}

func (p *Plan) planJournal() error {
	main := filepath.Join(p.Root, "journal.org")
	conflicts, _ := filepath.Glob(filepath.Join(p.Root, "journal.sync-conflict-*.org"))
	sort.Strings(conflicts)
	var sources []string
	if exists(main) {
		sources = append(sources, main)
	}
	sources = append(sources, conflicts...)
	if len(sources) == 0 {
		return nil
	}
	v := vault.Vault{Root: p.Root}
	days := map[string]*dayAcc{}
	// Day files that already exist (a previous run, or a journal.org that
	// syncthing resurrected) are merged into, never clobbered.
	existing, err := v.Days()
	if err != nil {
		return err
	}
	for _, d := range existing {
		acc := &dayAcc{title: vault.DayTitle(d.Date), seen: map[string]bool{}, merged: true}
		if doc, err := org.ParseFile(d.Path); err == nil && doc.Title() != "" {
			acc.title = doc.Title()
		}
		for _, e := range d.Entries {
			acc.seen[e.Title+"\x00"+strings.Join(e.Body, "\n")] = true
			acc.entries = append(acc.entries, e)
		}
		days[d.Date.Format("2006-01-02")] = acc
	}
	for _, src := range sources {
		doc, err := org.ParseFile(src)
		if err != nil {
			return err
		}
		for i, h := range doc.Headlines {
			if h.Level != 1 {
				continue
			}
			m := dateHead.FindStringSubmatch(h.Title)
			if m == nil {
				if org.Words(doc.Lines[h.ContentStart:h.End]) > 0 && !strings.EqualFold(h.Title, "Journal Entries") {
					p.warn("%s: skipped non-date headline %q", filepath.Base(src), h.Title)
				}
				continue
			}
			date := m[1]
			acc := days[date]
			if acc == nil {
				acc = &dayAcc{title: h.Title, seen: map[string]bool{}}
				days[date] = acc
			}
			// Entries are the level-2 headlines directly under the date.
			for _, e := range doc.Headlines[i+1:] {
				if e.Line >= h.End {
					break
				}
				if e.Level != 2 {
					continue
				}
				body := promote(doc.Lines[e.ContentStart:e.End])
				key := e.Title + "\x00" + strings.Join(body, "\n")
				if acc.seen[key] {
					continue
				}
				acc.seen[key] = true
				acc.changed = true
				acc.entries = append(acc.entries, vault.Entry{Title: e.Title, Body: body})
			}
			// Text directly under the date with no level-2 headline.
			if loose := trimBlank(linesUntilHeadline(doc, h)); len(loose) > 0 {
				key := "\x00" + strings.Join(loose, "\n")
				if !acc.seen[key] {
					acc.seen[key] = true
					acc.changed = true
					acc.entries = append(acc.entries, vault.Entry{Title: "Notes", Body: loose})
				}
			}
		}
	}
	for date, acc := range days {
		if acc.merged && !acc.changed {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err != nil {
			continue
		}
		if acc.merged {
			p.warn("journal/%s.org already existed; new entries appended, existing kept", date)
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "#+TITLE: %s\n", acc.title)
		for _, e := range acc.entries {
			sb.WriteString("\n* " + e.Title + "\n")
			for _, l := range e.Body {
				sb.WriteString(l + "\n")
			}
		}
		p.Writes[v.DayPath(t)] = sb.String()
	}
	for _, src := range sources {
		p.Moves = append(p.Moves, [2]string{src, filepath.Join(p.BakDir, filepath.Base(src))})
	}
	return nil
}

// linesUntilHeadline returns the lines between a headline's content start
// and the first sub-headline.
func linesUntilHeadline(doc *org.Doc, h org.Headline) []string {
	end := h.End
	for _, n := range doc.Headlines {
		if n.Line > h.Line && n.Line < end {
			end = n.Line
			break
		}
	}
	return doc.Lines[h.ContentStart:end]
}

// ---------- notes ----------

func (p *Plan) planNotes() error {
	zk := filepath.Join(p.Root, "zettelkasten")
	if !exists(zk) {
		return nil
	}
	v := vault.Vault{Root: p.Root}
	if entries, _ := os.ReadDir(v.NotesPath()); len(entries) > 0 {
		return fmt.Errorf("%s already has files; refusing to migrate notes into it", v.NotesPath())
	}
	var files []string
	err := filepath.WalkDir(zk, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".org") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		base := filepath.Base(f)
		if skipFiles[base] {
			p.warn("%s: not migrated (tiller replaces it); kept in backup", base)
			continue
		}
		doc, err := org.ParseFile(f)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(zk, f)
		folder := filepath.Dir(rel) // ".", "fleeting", "literature", "permanent", "code-snippets"
		if isSplitSource(doc, base) {
			p.splitFile(doc, base, folder)
		} else {
			p.wholeFile(doc, f, base, folder)
		}
	}
	p.Moves = append(p.Moves, [2]string{zk, filepath.Join(p.BakDir, "zettelkasten")})
	return nil
}

// isSplitSource: inbox files, and untitled files made only of captured
// headlines, become one note per headline.
func isSplitSource(doc *org.Doc, base string) bool {
	if strings.HasSuffix(base, "-inbox.org") {
		return true
	}
	if doc.Title() != "" {
		return false
	}
	n := 0
	for _, h := range doc.Headlines {
		if h.Level == 1 {
			if _, ok := h.Props["CREATED"]; !ok {
				return false
			}
			n++
		}
	}
	return n > 0
}

func lifecycleTag(folder, base string) string {
	switch {
	case folder == "fleeting", strings.HasSuffix(base, "-inbox.org") && base != "literature-inbox.org" && base != "permanent-inbox.org":
		return vault.FleetingTag
	case folder == "literature", base == "literature-inbox.org":
		return "literature"
	}
	return ""
}

func (p *Plan) splitFile(doc *org.Doc, base, folder string) {
	fileTags := cleanTags(doc.FileTags())
	for _, h := range doc.Headlines {
		if h.Level != 1 {
			continue
		}
		createdRaw := strings.TrimSpace(h.Props["CREATED"])
		created, ok := org.ParseStamp(createdRaw)
		if !ok {
			if org.Words(doc.Lines[h.ContentStart:h.End]) > 0 {
				p.warn("%s: dropped placeholder headline %q (no CREATED)", base, h.Title)
			}
			continue
		}
		title, stamp := org.StripStamp(h.Title)
		title = strings.TrimRight(strings.TrimSpace(title), ":")
		if title == "" {
			title = "Untitled " + created.Format("2006-01-02 15:04")
		}
		if stamp.IsZero() {
			stamp = created
		}
		body := trimBlank(promote(stripTrailer(doc.Lines[h.ContentStart:h.End])))
		tags := mergeTags(fileTags, cleanTags(h.Tags), cleanTags(org.SplitTags(h.Props["TAGS"])))
		if lt := lifecycleTag(folder, base); lt != "" {
			tags = mergeTags([]string{lt}, tags)
		}
		n := &note{title: title, tags: tags, created: createdRaw, body: body, srcFile: base, srcHeadline: h.Title}
		if dup := p.findDuplicate(n); dup != nil {
			p.warn("%s: %q is an exact duplicate of an earlier note; dropped", base, title)
			p.byHeadline[base+"::"+h.Title] = dup.id
			p.byHeadline[base+"::"+title] = dup.id
			continue
		}
		p.addNote(n)
		p.byHeadline[base+"::"+h.Title] = n.id
		p.byHeadline[base+"::"+title] = n.id
	}
}

func (p *Plan) wholeFile(doc *org.Doc, path, base, folder string) {
	title := doc.Title()
	stem := strings.TrimSuffix(base, ".org")
	created := ""
	if m := stemStamp.FindStringSubmatch(stem); m != nil {
		if t, err := time.ParseInLocation("20060102", m[1], time.Local); err == nil {
			created = t.Format("[2006-01-02 Mon]")
			if m[2] != "" {
				if t, err := time.ParseInLocation("20060102150405", m[1]+m[2], time.Local); err == nil {
					created = org.Stamp(t)
				}
			}
		}
		stem = stem[len(m[0]):]
	}
	if c, ok := doc.Keyword("CREATED"); ok && strings.TrimSpace(c) != "" {
		created = strings.TrimSpace(c)
	}
	if title == "" {
		title = titleFromStem(stem)
		p.warn("%s: no #+TITLE; using %q", base, title)
	}
	n := &note{title: title, created: created, srcFile: base}
	if doc.FileID != "" {
		n.id = doc.FileID
	}
	n.tags = cleanTags(doc.FileTags())
	if lt := lifecycleTag(folder, base); lt != "" && !hasTag(n.tags, lt) {
		n.tags = mergeTags([]string{lt}, n.tags)
	}
	for _, k := range doc.Keywords {
		switch k.Key {
		case "TITLE", "ID", "FILETAGS", "CREATED":
		default:
			n.keywords = append(n.keywords, k)
		}
	}
	n.body = trimBlank(doc.Lines[doc.BodyStart:])
	p.addNote(n)
	p.byFile[base] = n.id
}

// addNote assigns slug and id, resolving collisions, and records the note.
func (p *Plan) addNote(n *note) {
	slug := ""
	if n.id != "" && slugLike.MatchString(n.id) && !uuidLike.MatchString(n.id) {
		slug = n.id
	} else {
		slug = org.Slugify(n.title)
	}
	if slug == "" {
		slug = "note-" + org.Slugify(n.created)
	}
	base := slug
	for i := 2; p.hasSlug(slug); i++ {
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	n.slug = slug
	if n.id == "" {
		n.id = slug
	}
	p.notes = append(p.notes, n)
}

func (p *Plan) hasSlug(s string) bool {
	for _, n := range p.notes {
		if n.slug == s {
			return true
		}
	}
	return false
}

func (p *Plan) findDuplicate(n *note) *note {
	for _, o := range p.notes {
		if o.title == n.title && strings.Join(o.body, "\n") == strings.Join(n.body, "\n") {
			return o
		}
	}
	return nil
}

// ---------- links ----------

var fileLinkRe = regexp.MustCompile(`\[\[file:([^\]\[]+?)\](\[[^\]]*\])?\]`)

func (p *Plan) resolveLinks() {
	for _, n := range p.notes {
		for i, line := range n.body {
			n.body[i] = fileLinkRe.ReplaceAllStringFunc(line, func(m string) string {
				sm := fileLinkRe.FindStringSubmatch(m)
				target, desc := sm[1], sm[2]
				path, heading, _ := strings.Cut(target, "::")
				if strings.HasSuffix(path, "/") {
					p.warn("%s: directory link %q left as-is", n.srcFile, target)
					return m
				}
				base := filepath.Base(path)
				if heading != "" {
					if id, ok := p.byHeadline[base+"::"+strings.TrimPrefix(heading, "*")]; ok {
						return "[[id:" + id + "]" + desc + "]"
					}
				}
				if id, ok := p.byFile[base]; ok {
					if heading != "" {
						p.warn("%s: link %q kept file target but dropped heading search", n.srcFile, target)
					}
					return "[[id:" + id + "]" + desc + "]"
				}
				p.warn("%s: unresolved link %q left as-is", n.srcFile, target)
				return m
			})
		}
	}
}

// ---------- rendering ----------

func (p *Plan) renderNotes() {
	v := vault.Vault{Root: p.Root}
	for _, n := range p.notes {
		var sb strings.Builder
		fmt.Fprintf(&sb, ":PROPERTIES:\n:ID: %s\n:END:\n", n.id)
		fmt.Fprintf(&sb, "#+TITLE: %s\n", n.title)
		if len(n.tags) > 0 {
			fmt.Fprintf(&sb, "#+FILETAGS: %s\n", org.JoinTags(n.tags))
		}
		if n.created != "" {
			fmt.Fprintf(&sb, "#+CREATED: %s\n", n.created)
		}
		for _, k := range n.keywords {
			fmt.Fprintf(&sb, "#+%s: %s\n", k.Key, k.Value)
		}
		sb.WriteString("\n")
		for _, l := range n.body {
			sb.WriteString(l + "\n")
		}
		p.Writes[filepath.Join(v.NotesPath(), n.slug+".org")] = sb.String()
	}
}

// ---------- apply ----------

func (p *Plan) Empty() bool { return len(p.Writes) == 0 && len(p.Moves) == 0 }

func (p *Plan) Apply() error {
	if p.Empty() {
		return nil
	}
	if err := os.MkdirAll(p.BakDir, 0o755); err != nil {
		return err
	}
	// Move originals first so a crash leaves them intact in the backup.
	for _, mv := range p.Moves {
		if err := os.MkdirAll(filepath.Dir(mv[1]), 0o755); err != nil {
			return err
		}
		if err := os.Rename(mv[0], mv[1]); err != nil {
			return fmt.Errorf("move %s: %w", mv[0], err)
		}
	}
	paths := make([]string, 0, len(p.Writes))
	for k := range p.Writes {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(p.Writes[path]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Summary lists planned writes and moves, root-relative.
func (p *Plan) Summary() string {
	var sb strings.Builder
	paths := make([]string, 0, len(p.Writes))
	for k := range p.Writes {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	for _, path := range paths {
		rel, _ := filepath.Rel(p.Root, path)
		fmt.Fprintf(&sb, "write  %s  (%d lines)\n", rel, strings.Count(p.Writes[path], "\n"))
	}
	for _, mv := range p.Moves {
		from, _ := filepath.Rel(p.Root, mv[0])
		to, _ := filepath.Rel(p.Root, mv[1])
		fmt.Fprintf(&sb, "move   %s  ->  %s\n", from, to)
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&sb, "warn   %s\n", w)
	}
	return sb.String()
}

// ---------- helpers ----------

func (p *Plan) warn(format string, a ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, a...))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// promote lifts every headline in lines one level (** -> *).
func promote(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.HasPrefix(l, "**") {
			out[i] = l[1:]
		} else {
			out[i] = l
		}
	}
	return out
}

// stripTrailer drops a trailing "---" separator and anything after it.
func stripTrailer(lines []string) []string {
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			return lines[:i]
		}
	}
	return lines
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// cleanTags drops organisational tags that the layout now expresses.
func cleanTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		switch strings.ToLower(t) {
		case "inbox", "permanent":
		default:
			out = append(out, t)
		}
	}
	return out
}

func mergeTags(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		for _, t := range g {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}

func titleFromStem(stem string) string {
	words := strings.FieldsFunc(stem, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
