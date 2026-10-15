package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/paulhmurray/tiller/internal/index"
	"github.com/paulhmurray/tiller/internal/vault"
)

func TestRender(t *testing.T) {
	root := t.TempDir()
	v := vault.Vault{Root: root}
	os.MkdirAll(v.NotesPath(), 0o755)
	os.WriteFile(filepath.Join(v.NotesPath(), "a.org"), []byte(":PROPERTIES:\n:ID: a\n:END:\n#+TITLE: Alpha\n\n</script><b>bold</b> [[id:b]] [[file:gone.org]]\n"), 0o644)
	os.WriteFile(filepath.Join(v.NotesPath(), "b.org"), []byte("#+TITLE: Beta\n"), 0o644)
	ix, err := index.Build(v)
	if err != nil {
		t.Fatal(err)
	}
	html, err := Render(ix, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	for _, want := range []string{`"id":"a"`, `"title":"Alpha"`, `"id":"missing:gone"`, `"source":"a","target":"b"`, "d3js.org v7"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// The body's </script> must have been escaped inside the JSON block.
	if strings.Count(s, "</script>") != 3 {
		t.Errorf("unexpected number of </script> tags: %d", strings.Count(s, "</script>"))
	}
}
