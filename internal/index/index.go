// Package index resolves the links between notes (and from journal days
// to notes) into a graph: outgoing links, backlinks, orphans, and targets
// that are referenced but not yet written.
package index

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/vault"
)

type Kind int

const (
	KindNote Kind = iota
	KindDay
)

type Node struct {
	ID      string // canonical id; for days "day:2006-01-02"
	Key     string // lower-cased ID, used for lookup
	Slug    string
	Title   string
	Kind    Kind
	Path    string
	Tags    []string
	Words   int
	Created time.Time
	ModTime time.Time
	Date    time.Time // days only
}

func (n *Node) HasTag(t string) bool {
	for _, x := range n.Tags {
		if x == t {
			return true
		}
	}
	return false
}

// Life is the zettelkasten lifecycle bucket used for colouring.
func (n *Node) Life() string {
	switch {
	case n.Kind == KindDay:
		return "day"
	case n.HasTag(vault.FleetingTag):
		return "fleeting"
	case n.HasTag("literature"):
		return "literature"
	case n.HasTag("hub"):
		return "hub"
	}
	return "permanent"
}

type Index struct {
	Nodes     map[string]*Node    // by Key
	Order     []string            // keys, notes newest-modified first then days newest first
	Outgoing  map[string][]string // key -> target keys (resolved), deduped, in link order
	Backlinks map[string][]string // key -> source keys
	Dangling  map[string][]string // missing target label -> source keys
	bySlug    map[string]*Node
}

func Build(v vault.Vault) (*Index, error) {
	notes, err := v.Notes()
	if err != nil {
		return nil, err
	}
	days, err := v.Days()
	if err != nil {
		return nil, err
	}
	ix := &Index{
		Nodes:     map[string]*Node{},
		Outgoing:  map[string][]string{},
		Backlinks: map[string][]string{},
		Dangling:  map[string][]string{},
		bySlug:    map[string]*Node{},
	}
	rawLinks := map[string][]string{}
	for _, n := range notes {
		id := n.ID
		if id == "" {
			id = n.Slug
		}
		node := &Node{ID: id, Key: strings.ToLower(id), Slug: n.Slug, Title: n.Title, Kind: KindNote, Path: n.Path,
			Tags: n.Tags, Words: n.Words, Created: n.Created, ModTime: n.ModTime}
		if !ix.add(node) {
			continue
		}
		ix.bySlug[strings.ToLower(n.Slug)] = node
		rawLinks[node.Key] = n.Links
	}
	for i := len(days) - 1; i >= 0; i-- {
		d := days[i]
		id := "day:" + d.Date.Format("2006-01-02")
		node := &Node{ID: id, Key: id, Slug: d.Date.Format("2006-01-02"), Title: d.Date.Format("2006-01-02 Monday"),
			Kind: KindDay, Path: d.Path, Words: d.Words, Date: d.Date}
		if ix.add(node) {
			rawLinks[node.Key] = d.Links
		}
	}
	for _, src := range ix.Order {
		seen := map[string]bool{}
		for _, raw := range rawLinks[src] {
			target, label, relevant := ix.Resolve(raw)
			if !relevant {
				continue
			}
			if target == nil {
				if !seen["!"+label] {
					seen["!"+label] = true
					ix.Dangling[label] = append(ix.Dangling[label], src)
				}
				continue
			}
			if target.Key == src || seen[target.Key] {
				continue
			}
			seen[target.Key] = true
			ix.Outgoing[src] = append(ix.Outgoing[src], target.Key)
			ix.Backlinks[target.Key] = append(ix.Backlinks[target.Key], src)
		}
	}
	return ix, nil
}

// add registers a node; it reports false when another file already
// claims the same id, in which case the first one wins.
func (ix *Index) add(n *Node) bool {
	if _, dup := ix.Nodes[n.Key]; dup {
		return false
	}
	ix.Nodes[n.Key] = n
	ix.Order = append(ix.Order, n.Key)
	return true
}

// Resolve maps a raw link target to a node. relevant is false for links
// tiller does not track (https, mailto, ...). When relevant and node is
// nil, label names the missing target.
func (ix *Index) Resolve(raw string) (node *Node, label string, relevant bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(strings.ToLower(raw), "id:"):
		id := strings.TrimSpace(raw[3:])
		if n, ok := ix.Nodes[strings.ToLower(id)]; ok {
			return n, "", true
		}
		return nil, "id:" + id, true
	case strings.HasPrefix(strings.ToLower(raw), "file:"):
		path, _, _ := strings.Cut(raw[5:], "::")
		path = strings.TrimSpace(path)
		if path == "" || strings.HasSuffix(path, "/") || !strings.HasSuffix(path, ".org") {
			return nil, "", false
		}
		slug := strings.TrimSuffix(filepath.Base(path), ".org")
		if n, ok := ix.bySlug[strings.ToLower(slug)]; ok {
			return n, "", true
		}
		return nil, slug, true
	}
	return nil, "", false
}

func (ix *Index) Get(key string) *Node { return ix.Nodes[strings.ToLower(key)] }

// Notes returns note nodes in Order.
func (ix *Index) Notes() []*Node {
	var out []*Node
	for _, k := range ix.Order {
		if n := ix.Nodes[k]; n.Kind == KindNote {
			out = append(out, n)
		}
	}
	return out
}

// Orphans are notes with no resolved links in or out.
func (ix *Index) Orphans() []*Node {
	var out []*Node
	for _, n := range ix.Notes() {
		if len(ix.Outgoing[n.Key]) == 0 && len(ix.Backlinks[n.Key]) == 0 {
			out = append(out, n)
		}
	}
	return out
}

// Missing lists distinct referenced-but-unwritten targets, sorted.
func (ix *Index) Missing() []string {
	out := make([]string, 0, len(ix.Dangling))
	for k := range ix.Dangling {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MissingFrom lists the missing targets referenced by one node.
func (ix *Index) MissingFrom(key string) []string {
	var out []string
	for label, srcs := range ix.Dangling {
		for _, s := range srcs {
			if s == key {
				out = append(out, label)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// Degree is resolved links in plus out.
func (ix *Index) Degree(key string) int {
	return len(ix.Outgoing[key]) + len(ix.Backlinks[key])
}

// LinkCount is the number of resolved edges.
func (ix *Index) LinkCount() int {
	n := 0
	for _, t := range ix.Outgoing {
		n += len(t)
	}
	return n
}

// Suggest ranks other notes that might relate to key: shared topical tags
// count double, shared title words once. Lifecycle tags, self and anything
// already linked in either direction are excluded.
func (ix *Index) Suggest(key string, n int) []*Node {
	me := ix.Get(key)
	if me == nil {
		return nil
	}
	skipTag := map[string]bool{vault.FleetingTag: true, "literature": true, "hub": true}
	myTags := map[string]bool{}
	for _, t := range me.Tags {
		if !skipTag[t] {
			myTags[t] = true
		}
	}
	myWords := titleWords(me.Title)
	linked := map[string]bool{me.Key: true}
	for _, k := range ix.Outgoing[me.Key] {
		linked[k] = true
	}
	for _, k := range ix.Backlinks[me.Key] {
		linked[k] = true
	}
	type scored struct {
		n     *Node
		score int
	}
	var out []scored
	for _, o := range ix.Notes() {
		if linked[o.Key] {
			continue
		}
		s := 0
		for _, t := range o.Tags {
			if myTags[t] {
				s += 2
			}
		}
		for w := range titleWords(o.Title) {
			if myWords[w] {
				s++
			}
		}
		if s > 0 {
			out = append(out, scored{o, s})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].n.Title < out[j].n.Title
	})
	var res []*Node
	for i := 0; i < len(out) && i < n; i++ {
		res = append(res, out[i].n)
	}
	return res
}

func titleWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) >= 4 {
			out[w] = true
		}
	}
	return out
}
