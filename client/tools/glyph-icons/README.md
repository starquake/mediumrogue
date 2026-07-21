# Glyph icons

On-map entity glyphs — the monster-kind and player-class icons drawn dark on
each entity's colored dot (`client/src/render/entities.ts`). Vendored from
[game-icons.net](https://game-icons.net).

- `svg/` — the source SVGs (one per glyph key).
- `gen-glyph-icons.mjs` — inlines their paths into
  `client/src/render/glyphIcons.ts` (background rects removed, fills stripped;
  a single fill is applied at draw time so Pixi v8's `Graphics.svg()` can render
  them with no asset pipeline).

Regenerate the checked-in output with **`make icons`** after swapping a source
icon; **`make icons-check`** (part of `make check`) fails if it drifts. This is
the same generate-and-check-in pattern as `protocol.gen.ts` (`make protocol`).

## Attribution

Icons © their authors, licensed **CC BY 3.0**
(<https://creativecommons.org/licenses/by/3.0/>):

| Icon | Author |
|---|---|
| crossed-swords, hood, pointy-hat, wolf-head, dragon-head, frozen-orb, spectre, kin-archer, hydra | Lorc |
| rat, shambling-zombie, goblin-head, serpent (cobra) | Delapouite |
| troll, skeleton | Skoll |

The attribution is also shown in-app on the start screen.
