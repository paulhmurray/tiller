// tiller is a personal command centre over a folder of org files.
//
//	tiller                 open the home screen
//	tiller jot [text...]   append a line to today's journal, or open it
//	tiller new <title...>  create a fleeting note and open it
//	tiller today           print today's journal path
//	tiller migrate [-n]    convert journal.org + zettelkasten/ to the vault layout
//	tiller graph [-o f] [--no-open]  render the link graph page and open it
//
// The root is $TILLER_ROOT, else ~/orgfiles. The editor is $EDITOR, else nvim.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/paulhmurray/tiller/internal/graph"
	"github.com/paulhmurray/tiller/internal/index"
	"github.com/paulhmurray/tiller/internal/migrate"
	"github.com/paulhmurray/tiller/internal/tui"
	"github.com/paulhmurray/tiller/internal/vault"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tiller:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	v := vault.Vault{Root: root()}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nvim"
	}
	if len(args) == 0 {
		return tui.Run(v, editor)
	}
	switch args[0] {
	case "jot":
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			p, _, err := v.EnsureDay(time.Now())
			if err != nil {
				return err
			}
			return open(editor, p, true)
		}
		p, err := v.Jot(time.Now(), text)
		if err != nil {
			return err
		}
		fmt.Println(rel(v.Root, p))
		return nil
	case "new":
		title := strings.TrimSpace(strings.Join(args[1:], " "))
		if title == "" {
			return fmt.Errorf("usage: tiller new <title>")
		}
		p, err := v.NewNote(title, nil, time.Now())
		if err != nil {
			return err
		}
		return openAt(editor, p, vault.WriteLine(p))
	case "today":
		fmt.Println(v.DayPath(time.Now()))
		return nil
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		dry := fs.Bool("n", false, "dry run: print the plan and change nothing")
		fs.BoolVar(dry, "dry-run", false, "same as -n")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		plan, err := migrate.Build(v.Root, time.Now())
		if err != nil {
			return err
		}
		if plan.Empty() {
			fmt.Println("nothing to migrate: no journal.org or zettelkasten/ under", v.Root)
			return nil
		}
		fmt.Print(plan.Summary())
		if *dry {
			fmt.Println("\ndry run; nothing changed")
			return nil
		}
		if err := plan.Apply(); err != nil {
			return err
		}
		fmt.Printf("\nmigrated; originals in %s\n", rel(v.Root, plan.BakDir))
		return nil
	case "graph":
		fs := flag.NewFlagSet("graph", flag.ContinueOnError)
		out := fs.String("o", "", "write the page here instead of ~/.cache/tiller/graph.html")
		noOpen := fs.Bool("no-open", false, "write the page but do not open a browser")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ix, err := index.Build(v)
		if err != nil {
			return err
		}
		path, err := graph.Generate(ix, *out)
		if err != nil {
			return err
		}
		fmt.Printf("%s  (%d notes, %d links, %d orphans, %d referenced but unwritten)\n",
			path, len(ix.Notes()), ix.LinkCount(), len(ix.Orphans()), len(ix.Missing()))
		if *noOpen {
			return nil
		}
		return graph.Open(path)
	case "book":
		if len(args) >= 3 && args[1] == "new" {
			fs := flag.NewFlagSet("book new", flag.ContinueOnError)
			target := fs.Int("target", 70000, "word target")
			rest := args[2:]
			var title []string
			for i := 0; i < len(rest); i++ {
				if rest[i] == "-target" || rest[i] == "--target" {
					if i+1 < len(rest) {
						fs.Parse(rest[i : i+2])
						i++
					}
					continue
				}
				title = append(title, rest[i])
			}
			b, err := v.NewBook(strings.Join(title, " "), *target, time.Now())
			if err != nil {
				return err
			}
			fmt.Printf("%s  (%s)\n", b.Title, rel(v.Root, b.Outline))
			return nil
		}
		books, err := v.Books()
		if err != nil {
			return err
		}
		for _, b := range books {
			fmt.Printf("%-30s %6d / %d words, %d chapters\n", b.Title, b.Words(), b.Target, len(b.Chapters))
		}
		return nil
	case "quote":
		text, who := tui.QuoteFor(time.Now())
		fmt.Printf("%s\n  — %s\n", text, who)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", args[0], usage)
}

const usage = `usage:
  tiller                 open the home screen
  tiller jot [text...]   append a line to today's journal, or open it
  tiller new <title...>  create a fleeting note and open it
  tiller today           print today's journal path
  tiller migrate [-n]    convert journal.org + zettelkasten/ to the vault layout
  tiller book            list books; book new <title> [-target N] starts one
  tiller quote           print today's quote
  tiller graph           render the link graph to ~/.cache/tiller/graph.html and open it
                         (-o path to write elsewhere, --no-open to just write)
`

func root() string {
	if r := os.Getenv("TILLER_ROOT"); r != "" {
		return r
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "orgfiles"
	}
	return filepath.Join(home, "orgfiles")
}

func open(editor, path string, atEnd bool) error {
	args := []string{}
	if atEnd {
		args = append(args, "+$")
	}
	args = append(args, path)
	c := exec.Command(editor, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func openAt(editor, path string, line int) error {
	c := exec.Command(editor, fmt.Sprintf("+%d", line), path)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
