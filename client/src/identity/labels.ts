// labels.ts (#428): turning a protocol class/species id into the word a player
// reads — "fighter" -> "Fighter", "dwarf" -> "Dwarf".
//
// DERIVED, not a lookup table. All six ids capitalize to exactly the name the
// start screen's cards already print, so a table here would be a second copy of
// the vocabulary with nothing keeping the two in step — add a class one day and
// a lookup silently renders the raw id (or, worse, an empty cell) while the
// derivation just works. labels.test.ts pins the six against the card names, so
// the day a new id does NOT capitalize cleanly, the build says so.

/**
 * displayLabel capitalizes a protocol id for display. Returns "" unchanged, so
 * a bundle that has not arrived yet renders nothing rather than the string
 * "Undefined".
 */
export function displayLabel(id: string): string {
  if (id === "") {
    return "";
  }

  return id.charAt(0).toUpperCase() + id.slice(1);
}
