import { Container, Text, type Ticker } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const RISE_DURATION_MS = 1000;
const RISE_DISTANCE = HEX_SIZE * 1.1;
const HIT_ON_HOSTILE = 0xf0e6c8; // my/allied damage landing: bone white
const HIT_ON_PLAYER = 0xff6b5f; // damage to a player: alarm red

const NUMBER_STYLE = {
  fontFamily: "Courier New",
  fontSize: 15,
  fontWeight: "bold",
  stroke: { color: 0x0b0f0b, width: 3 },
} as const;

interface FloatingNumber {
  text: Text;
  originY: number;
  elapsed: number;
}

/**
 * Diablo-style floating combat numbers: "-N" rises from the victim's hex and
 * fades over ~1s of the playback window. main.ts derives the numbers by
 * diffing HP between consecutive turn bundles — the wire carries state, not
 * events, so the client reconstructs "what hit landed" from before/after.
 * Numbers over hostiles render bone-white (damage dealt), over players alarm
 * red (damage taken) — readable at a glance in a mixed brawl.
 */
export class DamageNumberLayer {
  readonly container = new Container();
  private floats: FloatingNumber[] = [];

  constructor(ticker: Ticker) {
    ticker.add(this.tick);
  }

  spawn(hex: Hex, amount: number, onPlayer: boolean): void {
    const { x, y } = hexToPixel(hex);
    const text = new Text({
      text: `-${amount}`,
      style: { ...NUMBER_STYLE, fill: onPlayer ? HIT_ON_PLAYER : HIT_ON_HOSTILE },
    });
    text.anchor.set(0.5, 1);
    // Nudge sideways a little by float count so simultaneous hits on one hex
    // (a stack, or melee + bow landing together) don't cover each other.
    const jitter = (this.floats.length % 3) - 1;
    text.position.set(x + jitter * HEX_SIZE * 0.35, y - HEX_SIZE * 0.5);
    this.container.addChild(text);
    this.floats.push({ text, originY: text.position.y, elapsed: 0 });
  }

  private tick = (ticker: Ticker): void => {
    for (const f of this.floats) {
      f.elapsed += ticker.deltaMS;
      const t = Math.min(1, f.elapsed / RISE_DURATION_MS);
      f.text.position.y = f.originY - RISE_DISTANCE * t;
      f.text.alpha = 1 - t * t; // hold near-full, then drop off
    }

    const done = this.floats.filter((f) => f.elapsed >= RISE_DURATION_MS);
    if (done.length > 0) {
      for (const f of done) {
        f.text.destroy();
      }
      this.floats = this.floats.filter((f) => f.elapsed < RISE_DURATION_MS);
    }
  };
}
