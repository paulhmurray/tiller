package org

import "testing"

func TestParse(t *testing.T) {
	d := Parse([]string{
		":PROPERTIES:", ":ID: abc", ":END:",
		"#+title: Hello", "#+FILETAGS: :a:b:", "",
		"body line with [[file:x.org][X]] and [[id:zz]]",
		"* [2026-01-28 Wed 08:07] Head :t1:t2:",
		"  :PROPERTIES:", "  :CREATED: [2026-01-28 Wed 08:07]", "  :END:",
		"text", "** sub", "more", "* second",
	})
	if d.FileID != "abc" || d.Title() != "Hello" || len(d.FileTags()) != 2 {
		t.Fatalf("preamble: %+v", d)
	}
	if d.BodyStart != 6 {
		t.Errorf("BodyStart=%d", d.BodyStart)
	}
	if got := len(d.Links()); got != 2 {
		t.Errorf("links=%d", got)
	}
	if len(d.Headlines) != 3 {
		t.Fatalf("headlines=%d", len(d.Headlines))
	}
	h := d.Headlines[0]
	if h.Level != 1 || len(h.Tags) != 2 || h.Props["CREATED"] == "" || h.ContentStart != 11 || h.End != 14 {
		t.Errorf("headline: %+v", h)
	}
	title, ts := StripStamp(h.Title)
	if title != "Head" || ts.IsZero() || ts.Hour() != 8 {
		t.Errorf("StripStamp: %q %v", title, ts)
	}
	if Slugify("Go: Byte Slice!") != "go-byte-slice" {
		t.Errorf("slugify: %q", Slugify("Go: Byte Slice!"))
	}
}
