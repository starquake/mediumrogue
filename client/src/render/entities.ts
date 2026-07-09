import { Container, Graphics, Text, type Ticker } from "pixi.js";

import { ClassFighter, ClassMage, ClassRogue, EntityPlayer, type Entity } from "../protocol.gen";
import { hexToPixel, HEX_SIZE, type Point } from "./hex";

const OTHER_COLOR = 0xc8b458;
const ME_COLOR = 0x8fd0ff;
const PARTY_COLOR = 0x8fe08f;
const MONSTER_COLOR = 0xd6544f;
const HP_BAR_BG = 0x1a1f1a;
const HP_BAR_FG = 0x4fd66c;
const COMBAT_RING_COLOR = 0xffcc33;
const BADGE_STYLE = { fontFamily: "Courier New", fontSize: 13, fill: 0xe8f0e8 } as const;

// A one-letter glyph per class, drawn centered on a player's dot so classes
// are distinguishable at a glance. Monsters have no class (empty string on
// the wire) and simply get no glyph.
const CLASS_GLYPH: Record<string, string> = {
  [ClassFighter]: "F",
  [ClassRogue]: "R",
  [ClassMage]: "M",
};
const CLASS_LABEL_STYLE = {
  fontFamily: "Courier New",
  fontSize: 11,
  fontWeight: "bold",
  fill: 0x0b0f0b,
} as const;

interface Dot {
  gfx: Graphics;
  label: Text;
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
}

/**
 * The entity layer: one persistent dot per entity (keyed by id) plus a count
 * badge per stacked hex. On each turn bundle a dot's tween target is set to its
 * new hex; the ticker interpolates from the dot's current position over the
 * playback window, so a move glides instead of snapping. The server snapshot is
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
        const label = new Text({ text: CLASS_GLYPH[e.class] ?? "", style: CLASS_LABEL_STYLE });
        label.anchor.set(0.5);
        this.container.addChild(label);
        dot = {
          gfx,
          label,
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
        };
        this.dots.set(e.id, dot);
      } else {
        // Retarget from wherever the dot is right now → the new hex.
        dot.from = dot.current;
        dot.to = to;
        dot.elapsed = 0;
        dot.duration = playbackMs;
        dot.mine = mine;
        dot.hostile = hostile;
        dot.partymate = partymate;
        dot.hp = e.hp;
        dot.maxHp = e.maxHp;
        dot.inCombat = e.inCombat;
        dot.label.text = CLASS_GLYPH[e.class] ?? "";
      }

      this.drawDot(dot);
    }

    // Retire dots for entities no longer in the world.
    for (const [id, dot] of this.dots) {
      if (!present.has(id)) {
        dot.gfx.destroy();
        dot.label.destroy();
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
      dot.current = {
        x: dot.from.x + (dot.to.x - dot.from.x) * f,
        y: dot.from.y + (dot.to.y - dot.from.y) * f,
      };
      this.drawDot(dot);
    }
  };

  private drawDot(dot: Dot): void {
    const { x, y } = dot.current;
    const color = dot.hostile
      ? MONSTER_COLOR
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
    dot.label.position.set(x, y);

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
