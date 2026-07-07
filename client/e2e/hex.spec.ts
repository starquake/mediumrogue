import { expect, test } from "@playwright/test";

import { hexToPixel, pixelToHex } from "../src/render/hex";

// hex.ts is pure math (only a type import), so this runs in-process with no
// browser — a unit test wearing a Playwright hat.
test("pixelToHex inverts hexToPixel for a spread of hexes", () => {
  for (let q = -6; q <= 6; q++) {
    for (let r = -6; r <= 6; r++) {
      const round = pixelToHex(hexToPixel({ q, r }));
      expect(round).toEqual({ q, r });
    }
  }
});

test("pixelToHex snaps a near-center point to the right hex", () => {
  const center = hexToPixel({ q: 2, r: -1 });
  // Nudge a few pixels off-center; still inside the hex.
  expect(pixelToHex({ x: center.x + 3, y: center.y - 3 })).toEqual({ q: 2, r: -1 });
});
