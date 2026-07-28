#!/usr/bin/env bash
# board.sh — the ticket state baton, on the GitHub Project's Status field.
#
# State used to live in `needs:*` labels. It now lives in the Status field of
# the "mediumrogue" user Project, so the maintainer can drag a card (web or
# mobile) and Claude can set the same value from the CLI — one source of truth
# either way. `ready to merge` stays a PR LABEL and is not managed here.
#
# The opaque node ids below are why this file exists: they belong in one place
# rather than copy-pasted into every skill.
#
#   ./board.sh state <issue> "<Status>"   set an issue's state
#   ./board.sh get   <issue>              print an issue's state
#   ./board.sh list  "<Status>"           list issue numbers in that state
#   ./board.sh states                     print the valid state names
#
# Requires the `project` scope: gh auth refresh -s project
set -euo pipefail

PROJECT_NUMBER=3
PROJECT_OWNER=starquake
PROJECT_ID="PVT_kwHOAA_wQM4BeuXF"
STATUS_FIELD_ID="PVTSSF_lAHOAA_wQM4BeuXFzhZGbpQ"

# Status name -> single-select option id.
option_id() {
  case "$1" in
    Backlog)         echo 4534558a ;;
    Spec)            echo b13ea411 ;;
    Plan)            echo 3b093184 ;;
    Build)           echo d13d6a2e ;;
    "Your input")    echo 6e229673 ;;
    "Your sign-off") echo c55b7cb4 ;;
    Done)            echo 0805dd0d ;;
    *) echo "unknown state: $1" >&2; echo "valid: Backlog, Spec, Plan, Build, Your input, Your sign-off, Done" >&2; return 1 ;;
  esac
}

# Item id for an issue number, adding the issue to the project if it is missing
# (a hand-filed issue may never have been added).
item_id() {
  local issue="$1" id
  id=$(gh project item-list "$PROJECT_NUMBER" --owner "$PROJECT_OWNER" --limit 200 --format json \
        --jq ".items[] | select(.content.number==$issue) | .id" 2>/dev/null | head -1)
  if [ -z "$id" ]; then
    id=$(gh project item-add "$PROJECT_NUMBER" --owner "$PROJECT_OWNER" \
          --url "https://github.com/$PROJECT_OWNER/mediumrogue/issues/$issue" \
          --format json --jq .id)
  fi
  printf '%s' "$id"
}

case "${1:-}" in
  state)
    issue="$2"; want="$3"
    opt=$(option_id "$want")
    item=$(item_id "$issue")
    gh api graphql -f query="mutation{updateProjectV2ItemFieldValue(input:{
        projectId:\"$PROJECT_ID\", itemId:\"$item\", fieldId:\"$STATUS_FIELD_ID\",
        value:{singleSelectOptionId:\"$opt\"}}){projectV2Item{id}}}" >/dev/null
    echo "#$issue -> $want"
    ;;
  get)
    gh project item-list "$PROJECT_NUMBER" --owner "$PROJECT_OWNER" --limit 200 --format json \
      --jq ".items[] | select(.content.number==$2) | .status // \"(unset)\""
    ;;
  list)
    gh project item-list "$PROJECT_NUMBER" --owner "$PROJECT_OWNER" --limit 200 --format json \
      --jq ".items[] | select(.status==\"$2\") | .content.number" | sort -n
    ;;
  states)
    printf '%s\n' Backlog Spec Plan Build "Your input" "Your sign-off" Done
    ;;
  *)
    sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
