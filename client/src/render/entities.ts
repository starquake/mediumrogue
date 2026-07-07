import { Container, Graphics, Text, type Ticker } from "pixi.js";

import type { Entity } from "../protocol.gen";
import { hexToPixel, HEX_SIZE, type Point } from "./hex";

const OTHER_COLOR = 0xc8b458;
const ME_COLOR = 0x8fd0ff;
const BADGE_STYLE = { fontFamily: "Courier New", fontSize: 13, fill: 0xe8f0e8 } as const;

interface Sprite {
  gfx: Graphics;
  badge: Text;
  from: Point;
  to: Point;
  current: Point;
  elapsed: number;
  duration: number;
  mine: boolean;
  count: number;
}

/**
 * The entity layer: one persistent Graphics per hex-stack, tweened between
 * turns. On each turn bundle we set every stack's tween target to its new
 * pixel position; the ticker interpolates over the playback window so moves
 * glide instead of snapping. The server snapshot is authoritative — a short or
 * dropped tween just means the sprite is already where the next bundle puts it.
 */
export class EntityLayer {
  readonly container = new Container();
  // Keyed by hex string "q,r": the entities standing there render as one
  // stack (top marker + count badge), matching the STACK_CAP rendering rule.
  private stacks = new Map<string, Sprite>();

  constructor(ticker: Ticker) {
    ticker.add(this.tick);
  }

  update(entities: Entity[], myEntityID: number, playbackMs: number): void {
    const byHex = new Map<string, Entity[]>();
    for (const e of entities) {
      const key = `${e.hex.q},${e.hex.r}`;
      byHex.set(key, [...(byHex.get(key) ?? []), e]);
    }

    // Retire stacks that no longer exist.
    for (const [key, sprite] of this.stacks) {
      if (!byHex.has(key)) {
        sprite.gfx.destroy();
        sprite.badge.destroy();
        this.stacks.delete(key);
      }
    }

    for (const [key, stack] of byHex) {
      const top = stack[0];
      if (top === undefined) {
        continue;
      }

      const to = hexToPixel(top.hex);
      const mine = stack.some((e) => e.id === myEntityID);
      let sprite = this.stacks.get(key);

      if (sprite === undefined) {
        const gfx = new Graphics();
        const badge = new Text({ text: "", style: BADGE_STYLE });
        this.container.addChild(gfx, badge);
        // New stack: appear in place (no tween).
        sprite = { gfx, badge, from: to, to, current: to, elapsed: 0, duration: 0, mine, count: stack.length };
        this.stacks.set(key, sprite);
      } else {
        sprite.from = sprite.current;
        sprite.to = to;
        sprite.elapsed = 0;
        sprite.duration = playbackMs;
        sprite.mine = mine;
        sprite.count = stack.length;
      }

      this.draw(sprite);
    }
  }

  private tick = (ticker: Ticker): void => {
    for (const sprite of this.stacks.values()) {
      if (sprite.current.x === sprite.to.x && sprite.current.y === sprite.to.y) {
        continue;
      }
      sprite.elapsed += ticker.deltaMS;
      const f = sprite.duration > 0 ? Math.min(1, sprite.elapsed / sprite.duration) : 1;
      sprite.current = {
        x: sprite.from.x + (sprite.to.x - sprite.from.x) * f,
        y: sprite.from.y + (sprite.to.y - sprite.from.y) * f,
      };
      this.draw(sprite);
    }
  };

  private draw(sprite: Sprite): void {
    const { x, y } = sprite.current;
    sprite.gfx
      .clear()
      .circle(x, y, HEX_SIZE * 0.45)
      .fill(sprite.mine ? ME_COLOR : OTHER_COLOR)
      .stroke({ width: 2, color: 0x0b0f0b });

    if (sprite.count > 1) {
      sprite.badge.text = `×${sprite.count}`;
      sprite.badge.position.set(x + HEX_SIZE * 0.3, y - HEX_SIZE * 0.9);
      sprite.badge.visible = true;
    } else {
      sprite.badge.visible = false;
    }
  }
}
