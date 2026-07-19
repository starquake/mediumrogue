package game //nolint:testpackage // white-box: reads the unexported registries; see rules_test.go.

import (
	"strings"
	"testing"
)

// guide_test.go (#156): the guide is generated so it cannot go stale. These
// tests pin the two ways it could go stale anyway — a registry entry the guide
// skips, and a vocabulary kind content uses but the guide never documents.

// TestGuideDataCoversEveryRegistryEntry: a new item or monster kind reaches the
// designer guide by existing, not by someone remembering to add it. That is
// the entire point of generating the document.
func TestGuideDataCoversEveryRegistryEntry(t *testing.T) {
	t.Parallel()

	g := GuideData()

	if got, want := len(g.Items), len(itemDefs); got != want {
		t.Errorf("guide items = %d, want every registered item (%d)", got, want)
	}

	if got, want := len(g.Monsters), len(monsterDefs); got != want {
		t.Errorf("guide monsters = %d, want every registered kind (%d)", got, want)
	}

	seen := make(map[string]bool, len(g.Items))
	for _, it := range g.Items {
		seen[it.ID] = true
	}

	for _, def := range itemDefs {
		if !seen[def.id] {
			t.Errorf("item %s is registered but missing from the guide", def.id)
		}
	}
}

// TestGuideDocumentsEveryVocabularyKindInUse: the fourth of the four places
// that must agree. A condition kind that content actually uses but the guide
// never describes is a hole in the document designers write cards against —
// and it would appear silently, since nothing else reads guideDescriptions.
func TestGuideDocumentsEveryVocabularyKindInUse(t *testing.T) {
	t.Parallel()

	check := func(owner string, cards []ruleCard) {
		t.Helper()

		for _, c := range cards {
			if _, ok := guideDescriptions[c.event]; !ok {
				t.Errorf("%s uses event %q, which the guide does not document", owner, c.event)
			}

			if _, ok := guideDescriptions[c.then.kind]; !ok {
				t.Errorf("%s uses effect %q, which the guide does not document", owner, c.then.kind)
			}

			for _, cond := range c.when {
				if _, ok := guideDescriptions[cond.kind]; !ok {
					t.Errorf("%s uses condition %q, which the guide does not document", owner, cond.kind)
				}
			}
		}
	}

	for _, def := range itemDefs {
		check("item "+def.id, def.rules)
	}

	for _, def := range monsterDefs {
		check("kind "+def.id, def.rules)
	}

	for _, def := range skillDefs {
		check("skill "+def.id, def.rules)
	}
}

// TestGuideStatsComeFromStatlines: the guide renders stat text through the SAME
// path as a tooltip. A second renderer could be internally consistent and still
// describe a game nobody is playing — the failure this file exists to prevent,
// one level up from the stale-PDF problem.
func TestGuideStatsComeFromStatlines(t *testing.T) {
	t.Parallel()

	for _, it := range GuideData().Items {
		def, ok := itemDefByID[it.ID]
		if !ok {
			t.Fatalf("guide item %s is not in the registry", it.ID)
		}

		want := statViewsFor(def)
		if got := len(it.Stats); got != len(want) {
			t.Errorf("%s guide stats = %d lines, want statViewsFor's %d", it.ID, got, len(want))

			continue
		}

		for i, w := range want {
			if got := it.Stats[i].Text; got != w.Text {
				t.Errorf("%s stat %d = %q, want statViewsFor's %q", it.ID, i, got, w.Text)
			}

			if got := it.Stats[i].Drawback; got != w.Drawback {
				t.Errorf("%s stat %d drawback = %v, want %v", it.ID, i, got, w.Drawback)
			}
		}
	}
}

// TestGuideCalibrationNumbersAreLive: the anchors come from the constants, so
// a rebalance moves the guide with it. Pinning the SHAPE (non-empty, described,
// no zero placeholders) rather than the values, which are free to move.
func TestGuideCalibrationNumbersAreLive(t *testing.T) {
	t.Parallel()

	numbers := GuideData().Numbers
	if len(numbers) == 0 {
		t.Fatal("guide has no calibration numbers")
	}

	for _, n := range numbers {
		if n.Value == 0 {
			t.Errorf("calibration %q = 0, want a live constant", n.Name)
		}

		if strings.TrimSpace(n.Description) == "" {
			t.Errorf("calibration %q has no description", n.Name)
		}
	}
}

// TestGuideMonsterAggroRadiusIsEffective: a kind that sets no override shows
// the global default, not 0. A designer pitching a new kind compares against
// what monsters really do; "0" would read as "never notices you".
func TestGuideMonsterAggroRadiusIsEffective(t *testing.T) {
	t.Parallel()

	for _, m := range GuideData().Monsters {
		if m.AggroRadius <= 0 {
			t.Errorf("kind %s guide aggro radius = %d, want the effective radius", m.ID, m.AggroRadius)
		}
	}
}
