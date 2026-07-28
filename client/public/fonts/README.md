# Font assets (#314)

**Sources:** two OFL families from Google Fonts, one display and one body. They
replace `"Courier New", monospace` — the client's only font until now, and one
nobody ever chose on purpose.

| File | Family | Role |
|---|---|---|
| `cinzel-latin-400.woff2` | [Cinzel](https://fonts.google.com/specimen/Cinzel) | display — the game title and panel headers |
| `literata-latin-400.woff2` | [Literata](https://fonts.google.com/specimen/Literata) | body — everything else, DOM and canvas |

**Licence:** SIL Open Font License 1.1 for both. Each family's licence ships
verbatim beside these files (`OFL-Cinzel.txt`, `OFL-Literata.txt`). The OFL
requires no attribution, but this repo credits its CC0 sounds too, so both
families are named on the start screen and in the controls overlay.

## Why these two

The pairing was chosen from mockups on #314 (`docs/mockups/2026-07-27-font-*.png`)
and picked by the maintainer. Literata was drawn for e-readers, so it stays
legible at the 11 px the world's name labels use — the tightest typographic
constraint in the game — while reading as a book rather than a terminal. Cinzel
carries the fantasy on a handful of strings that never have to be read small.

The body face is **proportional**. Monospace was never a requirement; it was a
property of the Courier default. Losing it costs two things Courier gave for
free, both handled in `index.html`: numeric readouts no longer form a column
(fixed with `font-variant-numeric: tabular-nums lining-nums` on the numeric
surfaces), and digits are no longer uniform width (same fix).

## Why latin-subset, one weight, WOFF2

The client is embedded into the Go binary (`//go:embed all:dist`), so every file
here inflates both the binary and a player's first download. These are Google's
**latin subsets** (U+0000–00FF plus common punctuation) at a single weight —
14 KB + 20 KB, against a ~676 KB client bundle and ~368 KB of audio. The full
multi-weight families, or the variable versions with their optical-size axes,
would be several times that for glyphs and weights the UI never renders.

They are committed rather than fetched at build time, for the same reason the
audio is: a build is never at the mercy of a third-party host. It is also a hard
requirement here — `internal/server/middleware.go` sets `default-src 'self'` with
no `font-src`, so a Google Fonts link would be blocked by the CSP and the text
would silently fall back.

## How they are wired

`client/index.html` declares both with `@font-face` pointing at `/fonts/…`.
Vite copies `public/` to the dist root verbatim and unfingerprinted, so that path
is stable — and the CSS lives in an inline `<style>` block, which Vite does not
rewrite `url()` inside, so a stable path is required rather than merely
convenient.

Canvas text (PixiJS) does not inherit CSS: `render/damage.ts` and
`render/entities.ts` name the family directly, and `main.ts` awaits
`document.fonts.ready` before building the stage. That gate is load-bearing —
Pixi rasterises `Text` to a texture at construction and never restyles it, so an
entity label built before the font loads would keep the fallback face for as long
as that entity is on screen.

## Reproducing this set

The WOFF2 files are Google's own latin subsets, fetched from the URLs in the
CSS the Fonts API serves to a modern browser:

```
curl -H "User-Agent: <a current Chrome UA>" \
  "https://fonts.googleapis.com/css2?family=Cinzel&family=Literata&display=swap"
```

Take the `src:` URL from each family's `U+0000-00FF` block. The licence files
come from `github.com/google/fonts` (`ofl/cinzel/OFL.txt`, `ofl/literata/OFL.txt`).
