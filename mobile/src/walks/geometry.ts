import type { WalkInput, WalkVariant } from './types.ts';

export type MapWalk = {
  start: { lat: number; lon: number };
  lines: [number, number][][];
  selectedId?: number;
};

/** Decode one complete polyline6 segment, preserving independent segments. */
export function decodePolyline6(encoded: string): [number, number][] {
  if (encoded.length > 2_000_000) throw new Error('Route shape too large');
  let offset = 0, latitude = 0, longitude = 0;
  function delta(): number {
    let value = 0, shift = 0, chunk: number;
    do {
      if (offset >= encoded.length || shift > 30) throw new Error('Invalid polyline');
      chunk = encoded.charCodeAt(offset++) - 63;
      if (chunk < 0 || chunk > 63) throw new Error('Invalid polyline');
      value += (chunk & 31) * 2 ** shift;
      shift += 5;
    } while (chunk >= 32);
    return value % 2 ? -(value + 1) / 2 : value / 2;
  }
  const points: [number, number][] = [];
  while (offset < encoded.length) {
    latitude += delta(); longitude += delta();
    if (Math.abs(latitude) > 90e6 || Math.abs(longitude) > 180e6) throw new Error('Invalid coordinate');
    points.push([longitude / 1e6, latitude / 1e6]);
  }
  if (points.length < 2) throw new Error('Incomplete route');
  return points;
}

export function mapWalk(request: WalkInput, variant: WalkVariant, selectedId?: number): { route: MapWalk; incomplete: boolean } {
  const lines: MapWalk['lines'] = [];
  let incomplete = !variant.geometry || variant.geometry.segments.length === 0;
  for (const segment of variant.geometry?.segments ?? []) {
    try { lines.push(decodePolyline6(segment)); } catch { incomplete = true; }
  }
  return { route: { start: request.start, lines, selectedId }, incomplete };
}
