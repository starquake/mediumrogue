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
PROJECT_REPO=mediumrogue
PROJECT_ID="PVT_kwHOAA_wQM4BeuXF"
STATUS_FIELD_ID="PVTSSF_lAHOAA_wQM4BeuXFzhZGbpQ"

# Status name -> single-select option id, looked up LIVE by name.
#
# These ids used to be hardcoded here, which broke the moment the board's
# columns were reordered (2026-07-28): reordering a single-select REPLACES every
# option, minting new ids and clearing every item's value. Names are the stable
# handle; ids are not. Resolving by name costs one API call per write and cannot
# go stale.
option_id() {
  local id
  id=$(gh api graphql -f query="{ user(login:\"$PROJECT_OWNER\"){ projectV2(number: $PROJECT_NUMBER){
        field(name:\"Status\"){ ... on ProjectV2SingleSelectField { options{ id name } } } } } }" \
      --jq ".data.user.projectV2.field.options[] | select(.name==\"$1\") | .id" 2>/dev/null)
  if [ -z "$id" ]; then
    echo "unknown state: $1" >&2
    echo "valid: $(gh api graphql -f query="{ user(login:\"$PROJECT_OWNER\"){ projectV2(number: $PROJECT_NUMBER){
          field(name:\"Status\"){ ... on ProjectV2SingleSelectField { options{ name } } } } } }" \
        --jq '[.data.user.projectV2.field.options[].name] | join(", ")')" >&2
    return 1
  fi
  printf '%s' "$id"
}

# Item id for an issue number, adding the issue to the project if it is missing
# (a hand-filed issue may never have been added).
#
# Asks the ISSUE for its project items rather than listing the whole board.
# `gh project item-list` pulls every item with its nested content on every call,
# which is the single most expensive query here — enough of them exhausted the
# 5000-point GraphQL budget in one afternoon (2026-07-28) while the REST budget
# sat untouched at 4971/5000. This form costs a fraction of that.
item_id() {
  local issue="$1" id
  id=$(gh api graphql -f query="{ repository(owner:\"$PROJECT_OWNER\", name:\"$PROJECT_REPO\"){
        issue(number: $issue){ projectItems(first:10){ nodes{ id project{ number } } } } } }" \
      --jq ".data.repository.issue.projectItems.nodes[] | select(.project.number==$PROJECT_NUMBER) | .id" 2>/dev/null | head -1)
  if [ -z "$id" ]; then
    id=$(gh project item-add "$PROJECT_NUMBER" --owner "$PROJECT_OWNER" \
          --url "https://github.com/$PROJECT_OWNER/$PROJECT_REPO/issues/$issue" \
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
    gh api graphql -f query="{ repository(owner:\"$PROJECT_OWNER\", name:\"$PROJECT_REPO\"){
        issue(number: $2){ projectItems(first:10){ nodes{ project{ number }
          fieldValueByName(name:\"Status\"){ ... on ProjectV2ItemFieldSingleSelectValue { name } } } } } } }" \
      --jq ".data.repository.issue.projectItems.nodes[] | select(.project.number==$PROJECT_NUMBER) | .fieldValueByName.name // \"(unset)\""
    ;;
  list)
    gh project item-list "$PROJECT_NUMBER" --owner "$PROJECT_OWNER" --limit 200 --format json \
      --jq ".items[] | select(.status==\"$2\") | .content.number" | sort -n
    ;;
  states)
    gh api graphql -f query="{ user(login:\"$PROJECT_OWNER\"){ projectV2(number: $PROJECT_NUMBER){
        field(name:\"Status\"){ ... on ProjectV2SingleSelectField { options{ name } } } } } }" \
      --jq '.data.user.projectV2.field.options[].name'
    ;;
  *)
    sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
