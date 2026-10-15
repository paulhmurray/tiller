// Package graph renders the link index as a standalone HTML page with an
// embedded d3 force layout. The page contains note bodies, so it is written
// to the per-user cache dir with 0600 and never into the synced vault.
package graph

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/index"
)

//go:embed assets/d3.min.js
var d3js string

//go:embed page.html
var page string

type jnode struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Kind    string   `json:"kind"` // note, day, missing
	Life    string   `json:"life"`
	Tags    []string `json:"tags,omitempty"`
	Degree  int      `json:"degree"`
	Words   int      `json:"words,omitempty"`
	Created string   `json:"created,omitempty"`
	Out     []string `json:"out,omitempty"`
	In      []string `json:"in,omitempty"`
	Body    string   `json:"body,omitempty"`
}

type jlink struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type payload struct {
	Generated string  `json:"generated"`
	Nodes     []jnode `json:"nodes"`
	Links     []jlink `json:"links"`
}

// Render builds the page. Days appear only when they link to something.
func Render(ix *index.Index, now time.Time) ([]byte, error) {
	p := payload{Generated: now.Format("Mon 2 Jan 15:04"), Nodes: []jnode{}, Links: []jlink{}}
	for _, key := range ix.Order {
		n := ix.Nodes[key]
		if n.Kind == index.KindDay && len(ix.Outgoing[key]) == 0 {
			continue
		}
		jn := jnode{ID: key, Title: n.Title, Life: n.Life(), Tags: n.Tags, Degree: ix.Degree(key), Words: n.Words,
			Out: ix.Outgoing[key], In: ix.Backlinks[key]}
		if n.Kind == index.KindDay {
			jn.Kind = "day"
		} else {
			jn.Kind = "note"
		}
		if !n.Created.IsZero() {
			jn.Created = n.Created.Format("2 Jan 2006")
		}
		if b, err := os.ReadFile(n.Path); err == nil {
			jn.Body = string(b)
		}
		for _, m := range ix.MissingFrom(key) {
			jn.Out = append(jn.Out, "missing:"+m)
		}
		p.Nodes = append(p.Nodes, jn)
		for _, t := range ix.Outgoing[key] {
			p.Links = append(p.Links, jlink{Source: key, Target: t})
		}
	}
	for _, m := range ix.Missing() {
		id := "missing:" + m
		srcs := ix.Dangling[m]
		p.Nodes = append(p.Nodes, jnode{ID: id, Title: strings.TrimPrefix(m, "id:"), Kind: "missing", Life: "missing", Degree: len(srcs), In: srcs})
		for _, s := range srcs {
			p.Links = append(p.Links, jlink{Source: s, Target: id})
		}
	}
	data, err := json.Marshal(p) // escapes <, > and & so bodies cannot close the script tag
	if err != nil {
		return nil, err
	}
	html := strings.Replace(page, "<!--DATA-->", string(data), 1)
	html = strings.Replace(html, "<!--D3-->", "<script>"+d3js+"</script>", 1)
	return []byte(html), nil
}

// DefaultPath is ~/.cache/tiller/graph.html.
func DefaultPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tiller", "graph.html"), nil
}

func Write(path string, html []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, html, 0o600)
}

// Open hands the file to the desktop's default browser and returns at once.
func Open(path string) error {
	bin, err := exec.LookPath("xdg-open")
	if err != nil {
		return fmt.Errorf("xdg-open not found; open %s yourself", path)
	}
	c := exec.Command(bin, path)
	c.Stdout, c.Stderr = nil, nil
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait() //nolint:errcheck
	return nil
}

// Generate renders, writes to path (DefaultPath when empty) and returns it.
func Generate(ix *index.Index, path string) (string, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return "", err
		}
		path = p
	}
	html, err := Render(ix, time.Now())
	if err != nil {
		return "", err
	}
	return path, Write(path, html)
}
