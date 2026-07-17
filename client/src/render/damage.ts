import { Container, Graphics, Text, type Ticker } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const RISE_DURATION_MS = 1000;
const RISE_DISTANCE = HEX_SIZE * 1.1;
const HIT_ON_HOSTILE = 0xf0e6c8; // my/allied damage landing: bone white
const HIT_ON_PLAYER = 0xff6b5f; // damage to a player: alarm red
const CRIT_COLOR = 0xffc94d; // gold — a crit moment pops regardless of faction
const GLANCE_COLOR = 0x9fc0d8; // pale steel — a glanced (halved) hit reads muted
const CRIT_BURST_DURATION_MS = 450;

const NUMBER_STYLE = {
  fontFamily: "Courier New",
  fontSize: 15,
  fontWeight: "bold",
  stroke: { color: 0x0b0f0b, width: 3 },
} as const;

/**
 * How a floating number renders (#114): a crit (an attacker-side
 * chance-conditioned boost fired — elf passive, Misericorde, Duelist's
 * Saber) rises bigger, gold, with a "!" and a one-shot burst ring; a glance
 * (the Rogue's defender-side halving) rises smaller and pale. Vocabulary is
 * crit/glance, never miss/dodge (docs/game-identity.md).
 */
export type HitStyle = "normal" | "crit" | "glance";

interface FloatingNumber {
  text: Text;
  originY: number;
  elapsed: number;
}

interface CritBurst {
  gfx: Graphics;
  x: number;
  y: number;
  elapsed: number;
}

/**
 * Diablo-style floating combat numbers: "-N" rises from the victim's hex and
 * fades over ~1s of the playback window. main.ts derives the numbers by
 * diffing HP between consecutive turn bundles — and, since #114, styles them
 * from the bundle's per-hit Hits view (crit/glance moments the delta alone
 * can't express). Numbers over hostiles render bone-white (damage dealt),
 * over players alarm red (damage taken) — readable at a glance in a mixed
 * brawl; crit gold and glance steel override either.
 */
export class DamageNumberLayer {
  readonly container = new Container();
  private floats: FloatingNumber[] = [];
  private bursts: CritBurst[] = [];

  constructor(ticker: Ticker) {
    ticker.add(this.tick);
  }

  spawn(hex: Hex, amount: number, onPlayer: boolean, style: HitStyle = "normal"): void {
    const { x, y } = hexToPixel(hex);
    const fill = style === "crit" ? CRIT_COLOR : style === "glance" ? GLANCE_COLOR : onPlayer ? HIT_ON_PLAYER : HIT_ON_HOSTILE;
    const text = new Text({
      text: style === "crit" ? `-${amount}!` : `-${amount}`,
      style: {
        ...NUMBER_STYLE,
        fill,
        fontSize: style === "crit" ? 22 : style === "glance" ? 12 : NUMBER_STYLE.fontSize,
        ...(style === "glance" ? { fontStyle: "italic" as const } : {}),
      },
    });
    text.anchor.set(0.5, 1);
    // Nudge sideways a little by float count so simultaneous hits on one hex
    // (a stack, or melee + bow landing together) don't cover each other.
    const jitter = (this.floats.length % 3) - 1;
    text.position.set(x + jitter * HEX_SIZE * 0.35, y - HEX_SIZE * 0.5);
    this.container.addChild(text);
    this.floats.push({ text, originY: text.position.y, elapsed: 0 });

    // A crit also fires a one-shot expanding burst ring on the victim's hex —
    // the "moment" reads even before the number is parsed.
    if (style === "crit") {
      const gfx = new Graphics();
      this.container.addChild(gfx);
      this.bursts.push({ gfx, x, y, elapsed: 0 });
    }
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

    for (const b of this.bursts) {
      b.elapsed += ticker.deltaMS;
      const t = Math.min(1, b.elapsed / CRIT_BURST_DURATION_MS);
      b.gfx
        .clear()
        .circle(b.x, b.y, HEX_SIZE * (0.35 + 0.75 * t))
        .stroke({ width: 3.5, color: CRIT_COLOR, alpha: 1 - t });
    }

    const burstsDone = this.bursts.filter((b) => b.elapsed >= CRIT_BURST_DURATION_MS);
    if (burstsDone.length > 0) {
      for (const b of burstsDone) {
        b.gfx.destroy();
      }
      this.bursts = this.bursts.filter((b) => b.elapsed < CRIT_BURST_DURATION_MS);
    }
  };
}
