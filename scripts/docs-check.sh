#!/usr/bin/env bash
# docs-check: every repo path a markdown doc mentions must exist.
#
# Two sweeps over all tracked *.md files (#125 follow-up — the docs get a
# check that validates THEM, not just a lint heartbeat):
#
#   1. Backtick-quoted repo paths (`docs/FEATURES.md`, `internal/game/rules.go`)
#      — root-relative, only fully-qualified filenames (a directory or a
#      glob like `docs/**` is not checked).
#   2. Relative markdown links ([text](../design.md)) — resolved against the
#      file's own directory, then the repo root; URLs and #anchors skipped.
#
# Rationale: retiring docs/superpowers/ (#121) required hand-hunting every
# stale path reference; renames of code files docs point at rot silently.
# This makes both failures loud, in CI and in `make check`.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0

while IFS= read -r f; do
  # docs/superpowers/ is the retired historical spec/plan tree (#121, deleted
  # by the workflow-overhaul PR): one-shot records full of references to
  # files that were live when they were written — checking history is
  # meaningless. Drop this exclusion once the tree is gone.
  case "$f" in docs/superpowers/*) continue ;; esac

  dir=$(dirname "$f")

  # Sweep 1: backtick-quoted root-relative repo paths with an extension.
  while IFS= read -r p; do
    if [ ! -e "$p" ]; then
      echo "$f: dead repo path \`$p\`"
      fail=1
    fi
  done < <(grep -o '`[^`]*`' "$f" | tr -d '`' |
    grep -E '^(docs|internal|cmd|client|test|scripts|\.github|\.claude)/[A-Za-z0-9._/-]+\.(md|go|ts|tsx|js|json|yml|yaml|png|svg|sh|txt|html|css|mod|sum)$' || true)

  # Sweep 2: relative markdown link targets.
  while IFS= read -r target; do
    t=${target%%#*}   # drop anchor
    t=${t%% *}        # drop optional "title"
    [ -z "$t" ] && continue
    case "$t" in
      http://* | https://* | mailto:*) continue ;;
    esac
    if [ ! -e "$dir/$t" ] && [ ! -e "$t" ]; then
      echo "$f: dead link ($target)"
      fail=1
    fi
  done < <(grep -oE '\]\([^)]+\)' "$f" | sed -E 's/^\]\(//; s/\)$//' || true)
done < <(git ls-files '*.md')

if [ "$fail" -ne 0 ]; then
  echo "docs-check: dead references found (fix the doc or the path)" >&2
  exit 1
fi

echo "docs-check: all markdown path references resolve"
