---
name: mockup
description: >
  Use whenever visual/looks-driven work needs its pre-approval mockup — "make
  a mockup of X", "show me what it would look like", "can I see a preview",
  "design the panel first", or from inside a design slice whose value is how
  it LOOKS (new panel, HUD element, map styling, filter, tooltip layout).
  Builds an HTML mockup, screenshots it with the repo's Playwright, commits
  the image under docs/mockups/, and embeds it inline in the design issue
  with the exact URL form that renders on this PRIVATE repo. The maintainer
  approves the screenshot BEFORE any real UI is built — trigger this before
  writing any UI code for looks-driven work, even if the user doesn't ask
  for a mockup explicitly.
---

Visual work gets a mockup approved **before** the real UI exists (a CRT
filter was once built and rejected post-hoc; the paper-doll inventory was
approved from a mockup and built once). You produce the mockup, the
screenshot, and the embed; the maintainer's approval of the picture is part
of the spec OK.

## Step 1 — Build the mockup

A self-contained HTML file in the scratchpad. Match the game's look (dark
`#1a1a24`-family background, monospace, the client's existing palette) so
the maintainer judges the design, not the placeholder styling.

## Step 2 — Screenshot with the repo's own Playwright

```js
// shot-tmp.mjs — MUST physically live in client/ while it runs: ESM
// resolves @playwright/test relative to the script FILE, not cwd; a
// scratchpad-resident script throws ERR_MODULE_NOT_FOUND. Copy in, run,
// delete.
import { chromium } from "@playwright/test";
const [, , htmlPath, outPath] = process.argv;
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 640, height: 400 } });
await page.goto("file://" + htmlPath);
await page.screenshot({ path: outPath });
await browser.close();
```

Chromium is already installed for e2e. Size the viewport to the content.
View the PNG yourself before shipping it — catch a blank render.

## Step 3 — Commit the image

`docs/mockups/<YYYY-MM-DD>-<name>.png` on the work branch (never straight to
`main`). Screenshots of mockups are small; the repo is the image host
because GitHub has no upload API for issue attachments.

**The image MUST reach `main` eventually, and the embed must be repointed at
`main` once it does.** An embed pointing at a work branch is a dangling
dependency: delete the branch and the picture on that ticket breaks forever.
This already happened in slow motion — `2026-07-17-hover-highlight-variants.png`
lived only on `design/hover-highlight-mockup` while #135 embedded it from
there, long after #135's feature shipped. So either let the image ride the
implementation PR to `main`, or land it in its own docs PR; then edit the
issue's embed from `raw/<branch>/…` to `raw/main/…` and the branch becomes
disposable.

## Step 4 — Embed with EXACTLY this URL form

```markdown
![mockup](https://github.com/starquake/mediumrogue/raw/<branch>/docs/mockups/<file>.png)
```

The repo is **private**, so the URL shape decides whether the image renders
(verified 2026-07-16, PR #120):

- `github.com/…/raw/…` — ✅ renders inline (redirects with the viewer's
  session token).
- `github.com/…/blob/…` — works as a click-through link only.
- `raw.githubusercontent.com/…` — ❌ broken icon for everyone; never use it
  in issue/PR markdown.

Put the embed in the design issue's **Mockup** section (or the PR/comment
under discussion, with the 🤖 attribution header if it's a comment).

## Step 5 — STOP for approval

Set `needs: your sign-off` on the design issue (removing any other `needs:*`
label: `gh issue edit <n> --add-label "needs: your sign-off" --remove-label
"<old>"`) — the embedded screenshot now awaits the maintainer's yes/no, and
the label is how they see it's their turn. The maintainer says yes/no to the
screenshot. No real UI code before that. Iterate the HTML + re-screenshot on
the same filename if they want changes (same URL keeps rendering the new
commit's blob at the branch ref) — the label stays put while they're the ones
deciding.
