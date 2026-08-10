import {
  ClassFighter,
  ClassMage,
  ClassRogue,
  MaxNameLen,
  SpeciesDwarf,
  SpeciesElf,
  SpeciesHuman,
  SystemSender,
} from "../protocol.gen";

// names.ts (#402): a slightly silly default name that fits the class and
// species you just picked, replacing the flat "traveler" everyone shared.
//
// GIVEN NAME reads the SPECIES, SURNAME reads the CLASS — the decomposition of
// the maintainer's own examples (Joe Fisticuffs, Simon Sparklebang, Khyrmin
// Shadowfart), and why this is 3 + 3 pools rather than 9 lists: every one of
// the nine combinations falls out of composition.
//
// The surname is built from two halves because flat pools cannot reach "a
// duplicate is highly unlikely" at a size anyone would hand-write. For k
// players and N combinations the collision odds are about k²/2N, so across 15
// friends:
//
//   20 given x 15 surnames          =    300  ->  ~37%
//   20 given x 15 prefix x 15 suffix =  4,500  ->  ~2.5%
//
// Prefix x suffix multiplies, so 30 words per class buys three orders of
// magnitude that 15 finished surnames cannot — and the mismatched joins are
// half the joke.
//
// Nothing here is seeded. The name is picked BEFORE joining, never feeds the
// simulation, and is a fixed string by the time the server sees it, so
// Math.random is correct rather than a determinism exception. The e2e suite
// pins its own names and ignores this module entirely (#395): a random test
// fixture would trade a deterministic wrong answer for a nondeterministic
// mostly-right one, which is harder to diagnose.

/**
 * PLACEHOLDER POOLS — @starquake writes the real words (#402 task 4).
 *
 * These exist so the machinery is testable and reviewable; they are not the
 * shipping content and this PR does not merge on them. Lengths are
 * representative so the MaxNameLen test means something.
 */
const GIVEN: Record<string, readonly string[]> = {
  [SpeciesHuman]: ["Joe", "Simon", "Martha", "Bert", "Doreen"],
  [SpeciesElf]: ["Khyrmin", "Aelra", "Thessily", "Faenor"],
  [SpeciesDwarf]: ["Brunhild", "Dorn", "Grimla", "Hagbard"],
};

/** Surname halves per class. Joined directly: prefix + suffix, no separator. */
const SURNAME_PREFIX: Record<string, readonly string[]> = {
  [ClassFighter]: ["Fisti", "Bash", "Punch", "Iron"],
  [ClassMage]: ["Spark", "Fizzle", "Boom", "Wobble"],
  [ClassRogue]: ["Shadow", "Sneak", "Quiet", "Back"],
};

const SURNAME_SUFFIX: Record<string, readonly string[]> = {
  [ClassFighter]: ["cuffs", "face", "well", "gut"],
  [ClassMage]: ["lebang", "wick", "quill", "bottom"],
  [ClassRogue]: ["fart", "thrift", "pocket", "stabbath"],
};

/** A source of randomness, injectable so tests can enumerate deterministically. */
export type Picker = () => number;

function choose<T>(pool: readonly T[], pick: Picker): T {
  const item = pool[Math.floor(pick() * pool.length)];
  if (item === undefined) {
    // Unreachable for a non-empty pool; the pools are validated by
    // TestEveryNameFits' equivalent in names.test.ts.
    throw new Error("names: empty pool");
  }

  return item;
}

/**
 * everyNameFor enumerates every name the generator can produce for one
 * class/species pair — the basis of the length and reserved-name checks, which
 * must hold for ALL combinations rather than for a sample.
 */
export function everyNameFor(cls: string, species: string): string[] {
  const given = GIVEN[species] ?? [];
  const prefixes = SURNAME_PREFIX[cls] ?? [];
  const suffixes = SURNAME_SUFFIX[cls] ?? [];
  const out: string[] = [];

  for (const g of given) {
    for (const p of prefixes) {
      for (const s of suffixes) {
        out.push(`${g} ${p}${s}`);
      }
    }
  }

  return out;
}

/** The class/species pairs the generator covers — all nine. */
export const ALL_PAIRS: ReadonlyArray<{ cls: string; species: string }> = [
  ClassFighter,
  ClassRogue,
  ClassMage,
].flatMap((cls) => [SpeciesHuman, SpeciesElf, SpeciesDwarf].map((species) => ({ cls, species })));

/**
 * generateName returns a name for this class and species. Falls back to the
 * old flat default only if a pool is missing, which validation should make
 * impossible — a thrown error on the start screen would block joining, and a
 * dull name will not.
 */
export function generateName(cls: string, species: string, pick: Picker = Math.random): string {
  const given = GIVEN[species];
  const prefixes = SURNAME_PREFIX[cls];
  const suffixes = SURNAME_SUFFIX[cls];

  if (given === undefined || prefixes === undefined || suffixes === undefined) {
    return FALLBACK_NAME;
  }

  return `${choose(given, pick)} ${choose(prefixes, pick)}${choose(suffixes, pick)}`;
}

/** What the field said before #402, kept only as the unreachable fallback. */
export const FALLBACK_NAME = "traveler";

/** Re-exported so the tests assert against the server's own limits. */
export { MaxNameLen, SystemSender };
