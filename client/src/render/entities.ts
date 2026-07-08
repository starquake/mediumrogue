import { Container, Graphics, Text, type Ticker } from "pixi.js";

import { EntityMonster, type Entity } from "../protocol.gen";
import { hexToPixel, HEX_SIZE, type Point } from "./hex";

const OTHER_COLOR = 0xc8b458;
const ME_COLOR = 0x8fd0ff;
const MONSTER_COLOR = 0xd6544f;
const BADGE_STYLE = { fontFamily: "Courier New", fontSize: 13, fill: 0xe8f0e8 } as const;

interface Dot {
  gfx: Graphics;
  from: Point;
  to: Point;
  current: Point;
  elapsed: number;
  duration: number;
  mine: boolean;
  hostile: boolean;
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

  update(entities: Entity[], myEntityID: number, playbackMs: number): void {
    const present = new Set<number>();

    for (const e of entities) {
      present.add(e.id);

      const to = hexToPixel(e.hex);
      const mine = e.id === myEntityID;
      const hostile = e.kind === EntityMonster;
      let dot = this.dots.get(e.id);

      if (dot === undefined) {
        // First sighting: appear in place, no tween.
        const gfx = new Graphics();
        this.container.addChild(gfx);
        dot = { gfx, from: to, to, current: to, elapsed: 0, duration: 0, mine, hostile };
        this.dots.set(e.id, dot);
      } else {
        // Retarget from wherever the dot is right now → the new hex.
        dot.from = dot.current;
        dot.to = to;
        dot.elapsed = 0;
        dot.duration = playbackMs;
        dot.mine = mine;
        dot.hostile = hostile;
      }

      this.drawDot(dot);
    }

    // Retire dots for entities no longer in the world.
    for (const [id, dot] of this.dots) {
      if (!present.has(id)) {
        dot.gfx.destroy();
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
    const color = dot.hostile ? MONSTER_COLOR : dot.mine ? ME_COLOR : OTHER_COLOR;
    dot.gfx
      .clear()
      .circle(x, y, HEX_SIZE * 0.45)
      .fill(color)
      .stroke({ width: 2, color: 0x0b0f0b });
  }
}
