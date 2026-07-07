import { Container, Graphics, Text } from "pixi.js";

import type { Entity } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const OTHER_COLOR = 0xc8b458;
const ME_COLOR = 0x8fd0ff;
const BADGE_STYLE = { fontFamily: "Courier New", fontSize: 13, fill: 0xe8f0e8 } as const;

/**
 * The entity layer: redrawn wholesale from each turn bundle. At ~15 players
 * a full redraw per 5s turn is nothing; per-entity scene-graph bookkeeping
 * can wait until playback tweens (milestone 4) need it.
 */
export class EntityLayer {
  readonly container = new Container();
  private readonly marks = new Graphics();
  private badges: Text[] = [];

  constructor() {
    this.container.addChild(this.marks);
  }

  update(entities: Entity[], myEntityID: number): void {
    this.marks.clear();
    for (const badge of this.badges) {
      badge.destroy();
    }
    this.badges = [];

    // Group per hex so a stacked hex draws one marker + a count badge —
    // the STACK_CAP=5 rendering rule from the plan.
    const byHex = new Map<string, Entity[]>();
    for (const e of entities) {
      const key = `${e.hex.q},${e.hex.r}`;
      byHex.set(key, [...(byHex.get(key) ?? []), e]);
    }

    for (const stack of byHex.values()) {
      const top = stack[0];
      if (top === undefined) {
        continue;
      }

      const center = hexToPixel(top.hex);
      const mine = stack.some((e) => e.id === myEntityID);

      this.marks
        .circle(center.x, center.y, HEX_SIZE * 0.45)
        .fill(mine ? ME_COLOR : OTHER_COLOR)
        .stroke({ width: 2, color: 0x0b0f0b });

      if (stack.length > 1) {
        const badge = new Text({ text: `×${stack.length}`, style: BADGE_STYLE });
        badge.position.set(center.x + HEX_SIZE * 0.3, center.y - HEX_SIZE * 0.9);
        this.container.addChild(badge);
        this.badges.push(badge);
      }
    }
  }
}
