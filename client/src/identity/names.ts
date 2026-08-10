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
 * The word pools (#402). Given names read the SPECIES, surname halves read the
 * CLASS.
 *
 * Sized for the collision target rather than for taste: 20 given x 15 prefix x
 * 15 suffix = 4,500 names per class/species pair, which puts a duplicate across
 * 15 players near 2.5%. The lists are short because prefix x suffix multiplies
 * — 30 words per class do the work 225 hand-written surnames would.
 *
 * Every combination is checked by names.test.ts against MaxNameLen in RUNES and
 * against the reserved sender name, so a long word fails the build rather than
 * a player's join. The current worst case is 8 + 1 + 7 + 8 = 24, exactly the
 * cap — a longer given name or surname half needs a shorter partner.
 */
const GIVEN: Record<string, readonly string[]> = {
  // Resolutely ordinary, which is the joke: the surname does the work.
  [SpeciesHuman]: [
    "Joe",
    "Simon",
    "Martha",
    "Bert",
    "Doreen",
    "Nigel",
    "Susan",
    "Clive",
    "Brenda",
    "Trevor",
    "Maureen",
    "Colin",
    "Sheila",
    "Derek",
    "Pauline",
    "Barry",
    "Glenys",
    "Keith",
    "Norma",
    "Dennis",
  ],
  [SpeciesElf]: [
    "Khyrmin",
    "Aelra",
    "Thessily",
    "Faenor",
    "Lirian",
    "Sylwen",
    "Ithil",
    "Naeryn",
    "Elowen",
    "Miriel",
    "Thandu",
    "Yrsil",
    "Faelis",
    "Aranel",
    "Nimros",
    "Elenwe",
    "Saelth",
    "Orophin",
    "Tinuvel",
    "Galad",
  ],
  [SpeciesDwarf]: [
    "Brunhild",
    "Dorn",
    "Grimla",
    "Hagbard",
    "Thrain",
    "Bofur",
    "Helga",
    "Durnak",
    "Ingrid",
    "Balgrim",
    "Yrsa",
    "Torvald",
    "Hilda",
    "Gudrun",
    "Snorri",
    "Astrid",
    "Halvard",
    "Bothild",
    "Ragna",
    "Ulfar",
  ],
};

/** Surname halves per class. Joined directly: prefix + suffix, no separator. */
const SURNAME_PREFIX: Record<string, readonly string[]> = {
  [ClassFighter]: [
    "Fisti",
    "Bash",
    "Punch",
    "Iron",
    "Skull",
    "Bone",
    "Shield",
    "Hammer",
    "Bloody",
    "Grim",
    "Stout",
    "Brick",
    "Ham",
    "Steel",
    "Oaken",
  ],
  [ClassMage]: [
    "Spark",
    "Fizzle",
    "Boom",
    "Wobble",
    "Glimmer",
    "Ember",
    "Frost",
    "Whisp",
    "Star",
    "Moon",
    "Cinder",
    "Puff",
    "Rune",
    "Zap",
    "Twinkle",
  ],
  [ClassRogue]: [
    "Shadow",
    "Sneak",
    "Quiet",
    "Back",
    "Silent",
    "Night",
    "Soft",
    "Slink",
    "Dark",
    "Creep",
    "Sly",
    "Nimble",
    "Pick",
    "Cut",
    "Whisper",
  ],
};

const SURNAME_SUFFIX: Record<string, readonly string[]> = {
  [ClassFighter]: [
    "cuffs",
    "face",
    "well",
    "gut",
    "basher",
    "crusher",
    "wall",
    "shanks",
    "hide",
    "thump",
    "guard",
    "belly",
    "fist",
    "jaw",
    "bottom",
  ],
  [ClassMage]: [
    "lebang",
    "wick",
    "quill",
    "bottom",
    "whistle",
    "fingers",
    "bottle",
    "spark",
    "hat",
    "beard",
    "mumble",
    "flare",
    "pocket",
    "dabble",
    "snap",
  ],
  [ClassRogue]: [
    "fart",
    "thrift",
    "pocket",
    "stabbath",
    "foot",
    "fingers",
    "blade",
    "step",
    "latch",
    "purse",
    "shadow",
    "knuckle",
    "hush",
    "grin",
    "snatch",
  ],
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
].flatMap((cls) =>
  [SpeciesHuman, SpeciesElf, SpeciesDwarf].map((species) => ({ cls, species })),
);

/**
 * generateName returns a name for this class and species. Falls back to the
 * old flat default only if a pool is missing, which validation should make
 * impossible — a thrown error on the start screen would block joining, and a
 * dull name will not.
 */
export function generateName(
  cls: string,
  species: string,
  pick: Picker = Math.random,
): string {
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
