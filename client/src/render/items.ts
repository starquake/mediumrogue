import { Container, Graphics } from "pixi.js";

import type { GroundItemView } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const ITEM_COLOR = 0xb266ff;
const ITEM_OUTLINE = { width: 1.5, color: 0x0b0f0b };
const ITEM_SIZE = HEX_SIZE * 0.3;

/**
 * The ground-loot layer: one small diamond marker per dropped item, keyed by
 * ground-item id (the item instance id — stable across turns until it's
 * picked up). Redrawn wholesale from TurnEvent.groundItems every turn bundle
 * — drops are rare and don't move, so (unlike EntityLayer's per-frame tween
 * machinery, built for constant motion) a marker just snaps to its hex.
 * Added to the world container UNDER the entity layer, so a player/monster
 * dot standing on a drop's hex is never hidden by it.
 */
export class GroundItemLayer {
  readonly container = new Container();
  private markers = new Map<number, Graphics>();

  update(items: GroundItemView[]): void {
    const present = new Set<number>();

    for (const item of items) {
      present.add(item.id);

      let gfx = this.markers.get(item.id);
      if (gfx === undefined) {
        gfx = new Graphics();
        this.container.addChild(gfx);
        this.markers.set(item.id, gfx);
      }

      const { x, y } = hexToPixel(item.hex);
      gfx
        .clear()
        .poly([x, y - ITEM_SIZE, x + ITEM_SIZE, y, x, y + ITEM_SIZE, x - ITEM_SIZE, y])
        .fill(ITEM_COLOR)
        .stroke(ITEM_OUTLINE);
    }

    // Drop markers for items no longer on the ground (picked up, or never
    // re-sent — either way, gone from the latest snapshot).
    for (const [id, gfx] of this.markers) {
      if (!present.has(id)) {
        gfx.destroy();
        this.markers.delete(id);
      }
    }
  }
}
