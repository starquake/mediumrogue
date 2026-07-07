import type { MapResponse } from "../protocol.gen";

/** Fetches the static world map. Called once at startup. */
export async function fetchMap(): Promise<MapResponse> {
  const resp = await fetch("/api/map");
  if (!resp.ok) {
    throw new Error(`GET /api/map failed: ${resp.status}`);
  }

  return (await resp.json()) as MapResponse;
}
