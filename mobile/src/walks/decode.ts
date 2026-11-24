import { ApiError } from '../api/errors.ts';
import type { RecalculatedWalk, WalkResult, WalkStop, WalkVariant } from './types.ts';

function invalid(): never {
  throw new ApiError('invalid_response', 'Сервер вернул некорректный маршрут. Повторите расчёт.');
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return invalid();
  return value as Record<string, unknown>;
}
function text(value: unknown): string { return typeof value === 'string' ? value : invalid(); }
function named(value: unknown): string { const result = text(value); return result.trim() ? result : invalid(); }
function number(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : invalid();
}
function integer(value: unknown): number { const result = number(value); return Number.isSafeInteger(result) ? result : invalid(); }
function boolean(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function array<T>(value: unknown, decode: (item: unknown) => T): T[] {
  return Array.isArray(value) ? value.map(decode) : invalid();
}
function coordinate(value: unknown, limit: number): number {
  return typeof value === 'number' && Number.isFinite(value) && Math.abs(value) <= limit ? value : invalid();
}
function date(value: unknown): string {
  const result = text(value);
  return /^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(result) && Number.isFinite(Date.parse(result)) ? result : invalid();
}
function stop(value: unknown): WalkStop {
  const v = object(value), point = object(v.location), leg = object(v.incoming_leg);
  const id = integer(v.place_id), arrival = date(v.arrival), departure = date(v.departure);
  if (id === 0 || Date.parse(departure) < Date.parse(arrival)) return invalid();
  return {
    place_id: id, name: named(v.name), location: { lat: coordinate(point.lat, 90), lon: coordinate(point.lon, 180) },
    arrival, departure, visit_seconds: integer(v.visit_seconds),
    incoming_leg: { walk_seconds: number(leg.walk_seconds), distance_m: number(leg.distance_m) },
    hours_unconfirmed: boolean(v.hours_unconfirmed), price_unconfirmed: boolean(v.price_unconfirmed),
  };
}
export function decodeWalkVariant(value: unknown): WalkVariant {
  const v = object(value), totals = object(v.totals), explanation = object(v.explanation);
  const stops = array(v.stops, stop), count = integer(totals.stops_count);
  if (count !== stops.length || new Set(stops.map(s => s.place_id)).size !== count) return invalid();
  const categories = Object.fromEntries(Object.entries(object(explanation.category_counts)).map(([key, value]) => [key, integer(value)]));
  // Geometry is optional presentation data. A broken shape must not hide the stop list.
  let geometry: WalkVariant['geometry'] = null;
  if (v.geometry && typeof v.geometry === 'object' && !Array.isArray(v.geometry)) {
    const shape = object(v.geometry);
    if (shape.format === 'polyline6' && Array.isArray(shape.segments) && shape.segments.every(s => typeof s === 'string')) {
      geometry = { format: 'polyline6', segments: shape.segments };
    }
  }
  return {
    variant_id: named(v.variant_id), profile: named(v.profile), labels: array(v.labels, named), stops,
    totals: {
      walk_seconds: number(totals.walk_seconds), visit_seconds: integer(totals.visit_seconds), total_seconds: number(totals.total_seconds),
      distance_m: number(totals.distance_m), cost_minor: integer(totals.cost_minor), currency: named(totals.currency),
      cost_complete: boolean(totals.cost_complete), stops_count: count, unverified_stops_count: integer(totals.unverified_stops_count),
    },
    explanation: {
      text: text(explanation.text), hours_unconfirmed_count: integer(explanation.hours_unconfirmed_count),
      prices_unconfirmed_count: integer(explanation.prices_unconfirmed_count), category_counts: categories,
    }, geometry,
  };
}

export function decodeRecalculatedWalk(value: unknown): RecalculatedWalk {
  const v = object(value), diff = object(v.diff);
  const result = decodeWalkResult({ seed: v.seed, timezone: v.timezone, variants: [v.variant] });
  function signed(value: unknown): number {
    return typeof value === 'number' && Number.isFinite(value) ? value : invalid();
  }
  function ids(value: unknown): number[] {
    return array(value, item => { const id = integer(item); return id > 0 ? id : invalid(); });
  }
  const cost = signed(diff.cost_minor_delta);
  if (!Number.isSafeInteger(cost)) return invalid();
  return {
    seed: result.seed, timezone: result.timezone, variant: result.variants[0],
    diff: {
      total_seconds_delta: signed(diff.total_seconds_delta), distance_m_delta: signed(diff.distance_m_delta),
      cost_minor_delta: cost, added_place_ids: ids(diff.added_place_ids), removed_place_ids: ids(diff.removed_place_ids),
      prices_confirmed: boolean(diff.prices_confirmed), text: text(diff.text),
    },
  };
}
export function decodeWalkResult(value: unknown): WalkResult {
  const v = object(value), timezone = named(v.timezone);
  try { new Intl.DateTimeFormat('ru-RU', { timeZone: timezone }).format(0); } catch { return invalid(); }
  const variants = array(v.variants, decodeWalkVariant);
  if (new Set(variants.map(v => v.variant_id)).size !== variants.length) return invalid();
  return { seed: integer(v.seed), timezone, variants };
}
