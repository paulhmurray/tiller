// Package org is a small line-oriented scanner for the subset of org-mode
// that tiller cares about: file keywords, a file-level property drawer,
// headlines with tags and property drawers, and links. It is deliberately
// not a full parser; bodies are kept as verbatim lines.
package org

import (
	"bufio"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type Doc struct {
	Path      string
	Lines     []string
	Keywords  []Keyword // #+KEY: value, in file order
	FileID    string    // :ID: from a file-level property drawer, or #+ID:
	Headlines []Headline
	// BodyStart is the index of the first line after the preamble
	// (leading blank lines, keywords and the file property drawer).
	BodyStart int
}

type Keyword struct {
	Key   string // upper-cased
	Value string
	Line  int
}

type Headline struct {
	Level int
	Title string // without stars and tags; may start with a timestamp
	Tags  []string
	Line  int               // index into Doc.Lines
	End   int               // exclusive; next headline of same or higher level, or len(Lines)
	Props map[string]string // upper-cased keys from a PROPERTIES drawer
	// ContentStart is the first line after the headline and its drawer.
	ContentStart int
}

type Link struct {
	Target string // e.g. "file:foo.org::*Heading" or "id:abc"
	Desc   string
	Line   int
	Start  int // byte offsets within the line
	End    int
}

var (
	headlineRe = regexp.MustCompile(`^(\*+)\s+(.*?)(?:\s+(:[^\s:]+(?::[^\s:]+)*:))?\s*$`)
	keywordRe  = regexp.MustCompile(`^#\+([A-Za-z_]+):\s*(.*)$`)
	propRe     = regexp.MustCompile(`^\s*:([A-Za-z_-]+):\s*(.*)$`)
	linkRe     = regexp.MustCompile(`\[\[([^\]\[]+?)\](?:\[([^\]]*)\])?\]`)
	stampRe    = regexp.MustCompile(`^[\[<](\d{4}-\d{2}-\d{2})(?:\s+\w+)?(?:\s+(\d{2}:\d{2}))?[\]>]\s*`)
)

func ParseFile(path string) (*Doc, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	d := Parse(lines)
	d.Path = path
	return d, nil
}

func Parse(lines []string) *Doc {
	d := &Doc{Lines: lines}
	// Preamble: blank lines, keywords, and one property drawer, until the
	// first line that is none of those.
	i := 0
	for i < len(lines) {
		l := lines[i]
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			i++
		case strings.HasPrefix(t, "#+"):
			if m := keywordRe.FindStringSubmatch(t); m != nil {
				d.Keywords = append(d.Keywords, Keyword{Key: strings.ToUpper(m[1]), Value: strings.TrimSpace(m[2]), Line: i})
			}
			i++
		case strings.EqualFold(t, ":PROPERTIES:"):
			props, next := parseDrawer(lines, i)
			if id, ok := props["ID"]; ok {
				d.FileID = id
			}
			i = next
		default:
			goto done
		}
	}
done:
	d.BodyStart = i
	if d.FileID == "" {
		if v, ok := d.Keyword("ID"); ok {
			d.FileID = v
		}
	}
	// Headlines.
	for j := d.BodyStart; j < len(lines); j++ {
		m := headlineRe.FindStringSubmatch(lines[j])
		if m == nil {
			continue
		}
		h := Headline{Level: len(m[1]), Title: strings.TrimSpace(m[2]), Line: j, Props: map[string]string{}}
		if m[3] != "" {
			h.Tags = SplitTags(m[3])
		}
		h.ContentStart = j + 1
		if j+1 < len(lines) && strings.EqualFold(strings.TrimSpace(lines[j+1]), ":PROPERTIES:") {
			h.Props, h.ContentStart = parseDrawer(lines, j+1)
		}
		d.Headlines = append(d.Headlines, h)
	}
	for k := range d.Headlines {
		h := &d.Headlines[k]
		h.End = len(lines)
		for _, n := range d.Headlines[k+1:] {
			if n.Level <= h.Level {
				h.End = n.Line
				break
			}
		}
	}
	return d
}

// parseDrawer parses a :PROPERTIES: drawer starting at lines[start].
// Returns the properties and the index after :END:. If no :END: is found
// the drawer is treated as empty and start+1 is returned.
func parseDrawer(lines []string, start int) (map[string]string, int) {
	props := map[string]string{}
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.EqualFold(t, ":END:") {
			return props, i + 1
		}
		if m := propRe.FindStringSubmatch(lines[i]); m != nil {
			props[strings.ToUpper(m[1])] = strings.TrimSpace(m[2])
		} else {
			break
		}
	}
	return map[string]string{}, start + 1
}

// Keyword returns the first value of a #+KEY (case-insensitive).
func (d *Doc) Keyword(key string) (string, bool) {
	key = strings.ToUpper(key)
	for _, k := range d.Keywords {
		if k.Key == key {
			return k.Value, true
		}
	}
	return "", false
}

func (d *Doc) Title() string {
	v, _ := d.Keyword("TITLE")
	return v
}

func (d *Doc) FileTags() []string {
	v, ok := d.Keyword("FILETAGS")
	if !ok {
		return nil
	}
	return SplitTags(v)
}

// Links returns every [[target][desc]] link in the document.
func (d *Doc) Links() []Link {
	var out []Link
	for i, l := range d.Lines {
		for _, m := range linkRe.FindAllStringSubmatchIndex(l, -1) {
			lk := Link{Line: i, Start: m[0], End: m[1], Target: l[m[2]:m[3]]}
			if m[4] >= 0 {
				lk.Desc = l[m[4]:m[5]]
			}
			out = append(out, lk)
		}
	}
	return out
}

// SplitTags turns ":a:b:" or "a b" into ["a","b"].
func SplitTags(s string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ':' || unicode.IsSpace(r) }) {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// JoinTags renders tags as ":a:b:"; empty input gives "".
func JoinTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return ":" + strings.Join(tags, ":") + ":"
}

// StripStamp removes a leading [2026-01-28 Wed 08:07] or <...> timestamp
// from a headline title and returns the title and the parsed time (zero
// if none).
func StripStamp(title string) (string, time.Time) {
	m := stampRe.FindStringSubmatch(title)
	if m == nil {
		return title, time.Time{}
	}
	rest := strings.TrimSpace(title[len(m[0]):])
	layout, s := "2006-01-02", m[1]
	if m[2] != "" {
		layout, s = "2006-01-02 15:04", m[1]+" "+m[2]
	}
	t, err := time.ParseInLocation(layout, s, time.Local)
	if err != nil {
		return rest, time.Time{}
	}
	return rest, t
}

// ParseStamp parses an org timestamp such as [2026-01-28 Wed 08:07].
func ParseStamp(s string) (time.Time, bool) {
	_, t := StripStamp(strings.TrimSpace(s))
	return t, !t.IsZero()
}

// Stamp renders an inactive org timestamp.
func Stamp(t time.Time) string {
	return t.Format("[2006-01-02 Mon 15:04]")
}

// Slugify produces a lowercase, hyphenated filename stem.
func Slugify(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// Words counts whitespace-separated words in the given lines.
func Words(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len(strings.Fields(l))
	}
	return n
}
