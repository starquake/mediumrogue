import type { Ticker } from "pixi.js";

import { mustGet } from "./dom";

type Phase = "playback" | "input";

/**
 * The turn timer: a DOM countdown bar (HTML-over-canvas, per plan §6). On each
 * turn bundle it restarts a local phase clock — a playback phase while the
 * result animates, then a draining input-window bar — re-synced every turn so
 * it can never drift from the server. Milestone 6 adds the combat-bubble
 * "waiting for: …" state; this is the single auto-advance state.
 */
export class TurnTimer {
  private readonly fill: HTMLElement;
  private readonly bar: HTMLElement;
  private elapsed = 0;
  private intervalMs = 0;
  private playbackMs = 0;

  constructor(ticker: Ticker) {
    this.bar = mustGet("turn-timer");
    this.fill = mustGet("turn-timer-fill");
    ticker.add(this.tick);
  }

  onTurn(intervalMs: number, playbackMs: number): void {
    this.intervalMs = intervalMs;
    this.playbackMs = playbackMs;
    this.elapsed = 0;
  }

  private tick = (ticker: Ticker): void => {
    if (this.intervalMs === 0) {
      return;
    }
    this.elapsed = Math.min(this.intervalMs, this.elapsed + ticker.deltaMS);

    const inPlayback = this.elapsed < this.playbackMs;
    const phase: Phase = inPlayback ? "playback" : "input";
    this.bar.dataset["phase"] = phase;

    if (inPlayback) {
      // Bar fills up while the move animates.
      const f = this.playbackMs > 0 ? this.elapsed / this.playbackMs : 1;
      this.fill.style.width = `${f * 100}%`;
    } else {
      // Bar drains over the input window.
      const inputMs = this.intervalMs - this.playbackMs;
      const left = this.intervalMs - this.elapsed;
      const f = inputMs > 0 ? left / inputMs : 0;
      this.fill.style.width = `${f * 100}%`;
    }
  };
}
