import { Container, Graphics } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { drawHexTile, HEX_SIZE } from "./hex";

// Pale ice — the "would walk here" hover, out of combat (#135). Same blue
// family as my dot, the destination ring, and the move-reach tint (a hover IS
// the would-be destination), reusing NAME_LABEL_MINE_COLOR's value.
const WALK_COLOR = 0xd6efff;
// Parchment — hovering my own hex, whose click is a wait/cancel (#135); the
// same colour FeedbackLayer's committed-wait marker uses.
const WAIT_COLOR = 0xe8e4d0;

export type HoverMoveKind = "walk" | "wait";

export interface HoverMoveTile {
  hex: Hex;
  kind: HoverMoveKind;
}

/**
 * The world (out-of-combat) hover highlight (#135): the single tile a click
 * would act on lights up — pale ice for "walk here", parchment for a wait on my
 * own hex — answering "is this click live?" where there was no feedback before.
 * Null in combat (the reach tints + #101 ember cover it there) and on
 * unwalkable ground (rock/water). Drawn UNDER AttackHighlightLayer so #101's
 * ember always wins where they would ever coincide. main.ts owns the routing
 * (walkability, my hex, inCombat); this layer only draws the one tile.
 */
export class HoverHighlightLayer {
  readonly container = new Container();
  private readonly gfx = new Graphics();

  constructor() {
    this.container.addChild(this.gfx);
  }

  update(tile: HoverMoveTile | null): void {
    this.gfx.clear();

    if (tile === null) {
      return;
    }

    const color = tile.kind === "wait" ? WAIT_COLOR : WALK_COLOR;
    drawHexTile(this.gfx, tile.hex, { size: HEX_SIZE - 2, color, fillAlpha: 0.14, strokeWidth: 2.5, strokeAlpha: 0.95 });
  }
}
