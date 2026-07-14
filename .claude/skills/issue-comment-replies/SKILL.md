---
name: issue-comment-replies
description: >
  Use whenever the user wants to catch up on or respond to GitHub issue/PR
  comments — "check the issues for new comments", "any comments to reply to?",
  "reply to the issue comments", "catch up on the issues", "did anyone comment
  on the design tickets", "see if there's anything to respond to". Scans this
  repo's open AND recently-closed issues and pull requests for comments that
  our account (@starquake) hasn't answered yet, decides which genuinely merit a
  reply, then AUTO-POSTS low-risk factual replies and DRAFTS substantive/design
  replies for the maintainer's OK. Every posted comment opens with the
  "🤖 Comment by Claude" attribution header. Trigger this even if the user
  doesn't say the word "skill" — any request to review, triage, or answer
  issue/PR comments should use it.
---

You triage new comments on this repo's GitHub issues and pull requests and
respond to the ones that merit a reply. Comments post under the maintainer's
own account (`@starquake`) via `gh`, so **every** reply you post must open with
the attribution header (below) and — because a public comment under someone
else's name is hard to take back — you split replies into *auto-post* (safe,
factual) and *draft-for-approval* (substantive, contestable). When unsure which
bucket a reply is in, it's the draft bucket.

## The attribution header (never omit it)

Every posted comment begins with this exact line, then a blank line, then the
reply body:

```
> 🤖 **Comment by Claude** (AI pair-programmer working with @starquake) — posted through @starquake's account.
```

This is required by CLAUDE.md: `gh` posts as @starquake, so readers must be able
to tell an AI wrote it.

## Step 1 — Gather unanswered comments

The goal is comments from *other people* that our account hasn't responded to
yet. Because both the human maintainer and Claude post as `@starquake`, "our
side has replied" simply means: a `@starquake` comment exists *after* the
comment in question.

1. List candidate threads (open + recently touched, issues AND PRs):
   ```bash
   gh issue list --state all --limit 40 --json number,title,updatedAt,state
   gh pr list    --state all --limit 30 --json number,title,updatedAt,state
   ```
   Focus on threads updated recently (say, the last ~2 weeks) — skip long-dead ones.
2. For each candidate thread, pull the comment timeline:
   ```bash
   gh issue view <n> --json title,author,body,comments
   # PRs work the same: gh pr view <n> --json title,author,body,comments
   ```
3. Identify **unanswered comments**: a comment authored by *someone other than
   `@starquake`* with **no `@starquake` comment after it** on that thread. The
   opening issue/PR body counts as the first "comment" — a freshly opened issue
   by someone else with no reply is fair game.
4. Skip: comments **authored by @starquake** (that's us — the maintainer's own
   words or a prior Claude reply), pure bot noise (dependabot/github-actions
   version bumps) unless a human asked something about them, and threads where
   `@starquake` already replied last.

Report what you found before replying — a short list of "thread → the unanswered
comment(s)" — so the maintainer sees the surface area.

## Step 2 — Decide which merit a reply

Not every unanswered comment needs one. Reply when the comment is one of:

- **A direct question / request for info** — something answerable about a
  mechanic, current status, how a system works, where something lives.
- **An @-mention** of @starquake (or of Claude / the AI pair-programmer).
- **A design proposal or technical claim** where engine-grounded feedback is
  useful — e.g. a proposal in TTRPG idiom that should be translated to the
  ARPG equivalent or pushed back on (see `docs/game-identity.md`,
  `docs/design.md`, and the CLAUDE.md "TTRPG ideas gate"). You
  give the engineering read; you do **not** make the gameplay decision.

Do **not** reply to: acknowledgements ("thanks", "👍", "sounds good"), comments
that restate a decision already recorded, things clearly addressed to a specific
person that only they can answer, or anything where a reply would just add noise.
Silence is a fine outcome — err toward not replying when a comment doesn't
clearly fall into the above.

## Step 3 — Sort each reply: auto-post vs draft

- **Auto-post (safe):** the reply is a *factual, grounded, non-contentious*
  answer — current status, how a mechanic works per the code/docs, "that's
  tracked in #NN / documented in `docs/…`", a simple confirmation, or a
  clarifying question back. High confidence, nothing a reasonable maintainer
  would want to reword.
- **Draft for approval (substantive):** design or technical *feedback*, any
  pushback, anything opinion-shaped or contestable, anything that could read as
  making a **decision** (decisions are the maintainer's), or anything you're not
  fully sure of. Design-proposal replies are almost always this bucket.

**When in doubt, draft.** The cost of holding a safe reply for a few seconds is
nothing; the cost of auto-posting a wrong or presumptuous one under @starquake's
name is real.

## Step 4 — Write, then post or draft

Ground every reply in the code and docs — read the relevant source/section
first rather than answering from memory (this repo's combat model is ARPG, XP
is presence-based, etc.; getting it wrong publicly is worse than not replying).
Keep replies concise and specific; link the issue/PR/doc you're citing.

**Auto-post** a safe reply immediately (header included):
```bash
gh issue comment <n> --body-file <file>   # or: gh pr comment <n> --body-file <file>
```
Use `--body-file` (write the comment to a temp file first) — multi-line markdown
with the `>` header breaks `--body "…"` shell-quoting.

**Draft** the substantive ones: show the maintainer each thread + the proposed
reply text, grouped together, and ask for an OK (or edits). Post only the ones
approved, the same way. Never post a substantive reply without that OK.

## Guardrails

- The attribution header goes on **every** posted comment, no exceptions.
- Never post a design **decision** — only engine-grounded observations, leaving
  the call to the maintainer/designer.
- Never impersonate the maintainer's own voice or opinions; you're the AI
  pair-programmer, and the header says so.
- Don't double-reply: if you (or the maintainer) already answered a comment,
  leave it.
- Prefer few, high-value replies over blanketing every thread.

## Example

**Unanswered comment (issue #60):** "Does the human's +XP% apply per-quest-holder
on a party completion, or once?"
→ *Merits a reply* (direct question), *auto-post* (factual, grounded).
Draft:
```
> 🤖 **Comment by Claude** (AI pair-programmer working with @starquake) — posted through @starquake's account.

Per holder — quest completion pays every current holder in full, and the human
`+XP%` passive applies to each holder's award independently (`internal/game/quest.go`,
the completion payout). So a full human party each gets the +50%.
```

**Unanswered comment (issue #57):** "What if shields gave a flat +2 AC while blocking?"
→ *Merits a reply* (design/technical claim), *draft for approval* (TTRPG idiom —
AC is a coupled to-hit stat this game doesn't use; translate to the ARPG
equivalent and explain why, per the TTRPG-ideas gate — but let @starquake make
the call). Present the draft; don't auto-post.
