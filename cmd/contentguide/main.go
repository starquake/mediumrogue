// Command contentguide renders the designer content guide from the live
// registries (#156).
//
// Output is Markdown so the maintainer can share the file directly (GitHub
// renders it, and it diffs in review); text/template rather than html/template
// because the output is not HTML and escaping would mangle the prose.
//
// The guide's DATA — the card vocabulary, the calibration anchors, every item
// and monster kind — comes from game.GuideData(), so it cannot drift from the
// game the way a hand-written document does. Its PROSE — the coupling tell,
// the drift cases, how to write a card up — is authored in guide.md.tmpl,
// because it is argument rather than data and regenerating it would lose the
// reasoning.
//
// Usage: contentguide [-out docs/content-guide/README.md]. `make guide`
// wraps it. Regenerate in the SAME PR as any change to a number the guide
// cites, exactly as FEATURES.md already works.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"text/template"

	"github.com/starquake/mediumrogue/internal/game"
)

//go:generate echo "run 'make guide' instead"

const (
	defaultOut  = "docs/content-guide/README.md"
	defaultTmpl = "docs/content-guide/guide.md.tmpl"

	// guidePerm is the rendered guide's mode: an ordinary committed doc.
	guidePerm = 0o600
)

func main() {
	out := flag.String("out", defaultOut, "path to write the rendered guide to")
	tmpl := flag.String("tmpl", defaultTmpl, "path to the guide template")

	flag.Parse()

	if err := run(*out, *tmpl); err != nil {
		fmt.Fprintln(os.Stderr, "contentguide:", err)
		os.Exit(1)
	}
}

// run renders tmplPath into out. The template path is its OWN flag rather
// than being derived from out's directory: `make guide-check` renders to a
// scratch path to compare against the committed guide, and deriving would
// send it looking for the template in the scratch directory.
func run(out, tmplPath string) error {
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	// Render to a buffer first: a template error must not leave a
	// half-written guide on disk, since the committed file is what reviewers
	// and designers read.
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, game.GuideData()); err != nil {
		return fmt.Errorf("render: %w", err)
	}

	if err := os.WriteFile(out, buf.Bytes(), guidePerm); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}

	return nil
}
