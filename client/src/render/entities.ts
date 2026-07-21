import { Container, Graphics, Text, type Ticker } from "pixi.js";

import { ClassFighter, ClassMage, ClassRogue, EntityPlayer, type Entity, type Hex } from "../protocol.gen";
import { GLYPH_ICON_SVG, GLYPH_ICON_VIEWBOX } from "./glyphIcons";
import { hexDistance, hexToPixel, HEX_SIZE, type Point } from "./hex";

// A new hex farther than this from a dot's previous one is a respawn (or any
// other server-side teleport), never a walk — one world/bubble turn only
// ever advances an entity a single step. Skipping the tween for a jump this
// big keeps the camera (which follows the dot) from panning across the whole
// map; it cuts straight to the new spot instead, matching a respawn's feel.
const TELEPORT_HEX_DISTANCE = 8;

// A move glides over this fixed, snappy window rather than the whole playback
// phase — one hex step should read as a quick, deliberate step, not a slow
// drift across the turn. Capped to the playback window in update() so a dot
// always settles before the input phase opens.
const MOVE_TWEEN_MS = 200;

// Cubic ease-in-out: a dot accelerates off its old hex and eases into the new
// one — reads far snappier than the previous linear glide.
function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2;
}

const OTHER_COLOR = 0xc8b458;
const ME_COLOR = 0x8fd0ff;
const PARTY_COLOR = 0x8fe08f;
// MONSTER_COLOR is the fallback for an unrecognized monsterKind (a future
// kind the client hasn't been taught yet, or a monster wire shape from an
// older/newer server) — the pre-6c flat monster color, kept so an unknown
// kind still reads unambiguously as "hostile", never mistaken for a player.
const MONSTER_COLOR = 0xd6544f;
const HP_BAR_BG = 0x1a1f1a;
const HP_BAR_FG = 0x4fd66c;
const COMBAT_RING_COLOR = 0xffcc33;
const BADGE_STYLE = { fontFamily: "Courier New", fontSize: 13, fill: 0xe8f0e8 } as const;

// Player name labels (item 8, playtest batch 2): always-on, styled like the
// count badge (same font/size), recolored by relationship to me — a
// partymate tints the same green as their dot, mine is a brighter shade of
// my own dot's blue (so it pops slightly above everyone else's), anyone
// else gets the neutral near-white the badge itself uses.
const NAME_LABEL_STYLE = { fontFamily: "Courier New", fontSize: 11, fill: 0xe8f0e8 } as const;
const NAME_LABEL_MINE_COLOR = 0xd6efff;
const NAME_LABEL_OTHER_COLOR = 0xe8f0e8;

// A game-icons.net glyph per class (keys into GLYPH_ICON_SVG), drawn dark and
// centered on a player's dot so classes are distinguishable at a glance — the
// icon successor to the old one-letter class label.
const CLASS_ICON: Record<string, string> = {
  [ClassFighter]: "fighter",
  [ClassRogue]: "rogue",
  [ClassMage]: "mage",
};

// Per-kind monster look (milestone 6c): a distinct dot color. The kind id
// (Entity.monsterKind) doubles as the glyph-icon key (GLYPH_ICON_SVG), so a
// troll reads differently from a rat well before either is close enough to
// matter. wolf keeps the pre-6c flat MONSTER_COLOR so its look doesn't change
// out from under existing players. An unrecognized kind (no KIND_STYLE entry)
// falls back to MONSTER_COLOR with no icon, mirroring CLASS_ICON's unknown-class
// fallback.
const KIND_STYLE: Record<string, { color: number }> = {
  rat: { color: 0x9a8a6a },
  wolf: { color: MONSTER_COLOR },
  ghoul: { color: 0x7cbf6a },
  troll: { color: 0xd68a3f },
  dragon: { color: 0xd63fc9 },
  // Content-expansion kinds (#266): a sickly olive goblin, bone-white
  // skeleton, icy-cyan frost wisp, and spectral-violet wraith — each
  // distinct from the kinds above and from the player dot colors.
  goblin: { color: 0x6f8f3f },
  skeleton: { color: 0xcfc9b0 },
  "frost-wisp": { color: 0x5fb8d6 },
  wraith: { color: 0x9a7fd0 },
  // Timed-effect foundation (#271): a venom-green serpent. It has a color but
  // no glyph icon yet (the safe hostile-dot fallback) — the effect-icon polish
  // is a later #271 slice.
  serpent: { color: 0x4faf6a },
};

interface Dot {
  gfx: Graphics;
  // glyph is the class/kind icon drawn dark on the dot (treatment A), or null
  // for an entity whose class/kind the client hasn't been taught. glyphKey is
  // the icon key it was built from, so it's only rebuilt when that changes.
  glyph: Graphics | null;
  glyphKey: string | null;
  // nameLabel is the always-on name tag above a PLAYER dot (item 8) — null
  // for a monster (monsters get hover info instead, item 13).
  nameLabel: Text | null;
  hex: Hex;
  from: Point;
  to: Point;
  current: Point;
  elapsed: number;
  duration: number;
  mine: boolean;
  hostile: boolean;
  partymate: boolean;
  hp: number;
  maxHp: number;
  inCombat: boolean;
  monsterKind: string;
}

// glyphIconKeyFor is the glyph-icon key a dot draws: the class icon for a
// player, or the kind icon for a monster (both key into GLYPH_ICON_SVG) — null
// for an entity whose class/kind the client hasn't been taught (no icon, just
// the dot), mirroring the old letter glyph's unknown-class fallback.
function glyphIconKeyFor(e: Entity): string | null {
  if (e.kind === EntityPlayer) {
    return CLASS_ICON[e.class] ?? null;
  }

  return GLYPH_ICON_SVG[e.monsterKind] !== undefined ? e.monsterKind : null;
}

// The glyph icon's on-screen footprint — a bit smaller than the dot's diameter
// (HEX_SIZE * 0.9) so it sits inside with a margin.
const GLYPH_ICON_PX = HEX_SIZE * 0.62;

// buildGlyphIcon renders a glyph key's vendored path (glyphIcons.ts) as a dark
// Graphics, pivoted at its center and scaled into the dot — drawn once per dot
// (and only when its key changes), then just repositioned each frame like the
// old label. Every path gets a dark fill so it reads as a silhouette on the
// dot's kind/relationship color (treatment A).
function buildGlyphIcon(key: string): Graphics {
  const g = new Graphics();
  const inner = GLYPH_ICON_SVG[key];
  if (inner === undefined) {
    return g; // unknown key — an empty glyph (callers only pass known keys)
  }

  const filled = inner.replace(/<path/g, '<path fill="#0b0f0b"');
  g.svg(`<svg viewBox="0 0 ${GLYPH_ICON_VIEWBOX} ${GLYPH_ICON_VIEWBOX}">${filled}</svg>`);
  g.pivot.set(GLYPH_ICON_VIEWBOX / 2, GLYPH_ICON_VIEWBOX / 2);
  g.scale.set(GLYPH_ICON_PX / GLYPH_ICON_VIEWBOX);

  return g;
}

/**
 * The entity layer: one persistent dot per entity (keyed by id) plus a count
 * badge per stacked hex. On each turn bundle a dot's tween target is set to its
 * new hex; the ticker interpolates from the dot's current position over a short
 * eased window (MOVE_TWEEN_MS, cubic ease-in-out), so a move reads as a snappy
 * step instead of a slow linear drift. The server snapshot is
 * authoritative — a short or dropped tween just lands the dot where the next
 * bundle already puts it, never a desync. Badges are static, drawn at the
 * final stacked-hex position; the moving dots underneath carry the motion.
 */
export class EntityLayer {
  readonly container = new Container();
  private dots = new Map<number, Dot>();
  private badges = new Map<string, Text>();

  constructor(ticker: Ticker) {
    ticker.add(this.tick);
  }

  update(entities: Entity[], myEntityID: number, myPartyID: number, playbackMs: number): void {
    const present = new Set<number>();

    for (const e of entities) {
      present.add(e.id);

      const to = hexToPixel(e.hex);
      const mine = e.id === myEntityID;
      // Anything not a player renders hostile — an unknown kind is safer shown
      // as a monster than mistaken for a friendly player (per the protocol doc).
      const hostile = e.kind !== EntityPlayer;
      const partymate = e.partyId !== 0 && e.partyId === myPartyID && e.id !== myEntityID;
      let dot = this.dots.get(e.id);

      if (dot === undefined) {
        // First sighting: appear in place, no tween.
        const gfx = new Graphics();
        this.container.addChild(gfx);
        const glyphKey = glyphIconKeyFor(e);
        const glyph = glyphKey !== null ? buildGlyphIcon(glyphKey) : null;
        if (glyph !== null) {
          this.container.addChild(glyph);
        }

        // Name label (item 8): PLAYERS only — monsters get hover info
        // instead (item 13). Created once per dot, moves with its tween.
        let nameLabel: Text | null = null;
        if (e.kind === EntityPlayer) {
          nameLabel = new Text({ text: e.name, style: { ...NAME_LABEL_STYLE } });
          nameLabel.anchor.set(0.5, 1);
          this.container.addChild(nameLabel);
        }

        dot = {
          gfx,
          glyph,
          glyphKey,
          nameLabel,
          hex: e.hex,
          from: to,
          to,
          current: to,
          elapsed: 0,
          duration: 0,
          mine,
          hostile,
          partymate,
          hp: e.hp,
          maxHp: e.maxHp,
          inCombat: e.inCombat,
          monsterKind: e.monsterKind,
        };
        this.dots.set(e.id, dot);
      } else {
        // A jump farther than one step is a respawn (or other server-side
        // teleport), never a walk — skip the tween and appear in place, so
        // the camera cuts to the new spot instead of panning there.
        const teleported = hexDistance(dot.hex, e.hex) > TELEPORT_HEX_DISTANCE;

        // Retarget from wherever the dot is right now → the new hex (or, for
        // a teleport, straight to it — from/current jump too, not just to).
        dot.from = teleported ? to : dot.current;
        dot.to = to;
        dot.current = teleported ? to : dot.current;
        dot.hex = e.hex;
        dot.elapsed = 0;
        dot.duration = Math.min(MOVE_TWEEN_MS, playbackMs);
        dot.mine = mine;
        dot.hostile = hostile;
        dot.partymate = partymate;
        dot.hp = e.hp;
        dot.maxHp = e.maxHp;
        dot.inCombat = e.inCombat;
        dot.monsterKind = e.monsterKind;
        // Rebuild the glyph icon only if the class/kind actually changed
        // (effectively never for a persistent id) — otherwise reuse it.
        const glyphKey = glyphIconKeyFor(e);
        if (glyphKey !== dot.glyphKey) {
          dot.glyph?.destroy();
          dot.glyph = glyphKey !== null ? buildGlyphIcon(glyphKey) : null;
          if (dot.glyph !== null) {
            this.container.addChild(dot.glyph);
          }
          dot.glyphKey = glyphKey;
        }
        if (dot.nameLabel !== null) {
          dot.nameLabel.text = e.name;
        }
      }

      this.drawDot(dot);
    }

    // Retire dots for entities no longer in the world.
    for (const [id, dot] of this.dots) {
      if (!present.has(id)) {
        dot.gfx.destroy();
        dot.glyph?.destroy();
        dot.nameLabel?.destroy();
        this.dots.delete(id);
      }
    }

    this.updateBadges(entities);
  }

  private updateBadges(entities: Entity[]): void {
    const counts = new Map<string, { hex: Entity["hex"]; n: number }>();
    for (const e of entities) {
      const key = `${e.hex.q},${e.hex.r}`;
      const entry = counts.get(key);
      if (entry === undefined) {
        counts.set(key, { hex: e.hex, n: 1 });
      } else {
        entry.n += 1;
      }
    }

    // Drop badges whose hex is no longer a stack of 2+.
    for (const [key, badge] of this.badges) {
      const entry = counts.get(key);
      if (entry === undefined || entry.n < 2) {
        badge.destroy();
        this.badges.delete(key);
      }
    }

    for (const [key, entry] of counts) {
      if (entry.n < 2) {
        continue;
      }

      const center = hexToPixel(entry.hex);
      let badge = this.badges.get(key);
      if (badge === undefined) {
        badge = new Text({ text: "", style: BADGE_STYLE });
        this.container.addChild(badge);
        this.badges.set(key, badge);
      }

      badge.text = `×${entry.n}`;
      badge.position.set(center.x + HEX_SIZE * 0.3, center.y - HEX_SIZE * 0.9);
    }
  }

  /**
   * My entity's current (per-frame interpolated) pixel position, or null before
   * my dot exists. The camera reads this each frame to follow the animating
   * sprite smoothly rather than snapping to its hex once per turn.
   */
  myPixel(): Point | null {
    for (const dot of this.dots.values()) {
      if (dot.mine) {
        return dot.current;
      }
    }

    return null;
  }

  private tick = (ticker: Ticker): void => {
    for (const dot of this.dots.values()) {
      if (dot.current.x === dot.to.x && dot.current.y === dot.to.y) {
        continue;
      }

      dot.elapsed += ticker.deltaMS;
      const f = dot.duration > 0 ? Math.min(1, dot.elapsed / dot.duration) : 1;
      const eased = easeInOutCubic(f);
      dot.current = {
        x: dot.from.x + (dot.to.x - dot.from.x) * eased,
        y: dot.from.y + (dot.to.y - dot.from.y) * eased,
      };
      this.drawDot(dot);
    }
  };

  private drawDot(dot: Dot): void {
    const { x, y } = dot.current;
    const color = dot.hostile
      ? (KIND_STYLE[dot.monsterKind]?.color ?? MONSTER_COLOR)
      : dot.mine
        ? ME_COLOR
        : dot.partymate
          ? PARTY_COLOR
          : OTHER_COLOR;
    dot.gfx
      .clear()
      .circle(x, y, HEX_SIZE * 0.45)
      .fill(color)
      .stroke({ width: 2, color: 0x0b0f0b });
    dot.glyph?.position.set(x, y);

    // Player name label (item 8): always on, above the dot (a fixed offset
    // regardless of whether the HP bar happens to be showing this frame, so
    // it doesn't jitter up/down as HP crosses full) — party-color-tinted for
    // a partymate, a brighter shade of my own dot color for mine, neutral
    // near-white (the badge's own color) for anyone else. Moves with the
    // tween since drawDot runs every animation frame while a dot is moving.
    if (dot.nameLabel !== null) {
      dot.nameLabel.position.set(x, y - HEX_SIZE * 1.05);
      dot.nameLabel.style.fill = dot.mine ? NAME_LABEL_MINE_COLOR : dot.partymate ? PARTY_COLOR : NAME_LABEL_OTHER_COLOR;
    }

    // A combat time bubble freezes its members — a ring around the dot lets
    // world players see a frozen fight in progress, distinct from a stopped
    // (but free) entity.
    if (dot.inCombat) {
      dot.gfx.circle(x, y, HEX_SIZE * 0.62).stroke({ width: 2, color: COMBAT_RING_COLOR });
    }

    // Damaged entities get a small HP bar above the dot; a full-HP entity
    // shows none, so the battlefield stays uncluttered until something's hurt.
    if (dot.maxHp > 0 && dot.hp < dot.maxHp) {
      const barWidth = HEX_SIZE * 0.9;
      const barHeight = HEX_SIZE * 0.16;
      const barX = x - barWidth / 2;
      const barY = y - HEX_SIZE * 0.85;
      const frac = Math.max(0, Math.min(1, dot.hp / dot.maxHp));
      dot.gfx
        .rect(barX, barY, barWidth, barHeight)
        .fill(HP_BAR_BG)
        .rect(barX, barY, barWidth * frac, barHeight)
        .fill(HP_BAR_FG);
    }
  }
}
