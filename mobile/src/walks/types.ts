export type WalkInput = {
  travel_mode?: 'walking' | 'cycling';
  max_distance_m?: number;
  max_stops?: number;
  city: string;
  start: { lat: number; lon: number };
  start_at: string;
  budget_minutes: number;
  pace: 'slow' | 'normal' | 'fast';
  categories: string[];
  ticket_budget_minor?: number;
  seed?: number;
  include_geometry: boolean;
};

export type WalkStop = {
  place_id: number;
  name: string;
  location: { lat: number; lon: number };
  arrival: string;
  departure: string;
  visit_seconds: number;
  incoming_leg: { walk_seconds: number; distance_m: number };
  hours_unconfirmed: boolean;
  price_unconfirmed: boolean;
};

export type WalkTotals = {
  walk_seconds: number;
  visit_seconds: number;
  total_seconds: number;
  distance_m: number;
  cost_minor: number;
  currency: string;
  cost_complete: boolean;
  stops_count: number;
  unverified_stops_count: number;
};

export type WalkVariant = {
  variant_id: string;
  profile: string;
  labels: string[];
  stops: WalkStop[];
  totals: WalkTotals;
  explanation: {
    text: string;
    hours_unconfirmed_count: number;
    prices_unconfirmed_count: number;
    category_counts: Record<string, number>;
  };
  geometry: { format: 'polyline6'; segments: string[] } | null;
};
export type WalkResult = { seed: number; timezone: string; variants: WalkVariant[] };

export type RecalculatedWalk = {
  seed: number;
  timezone: string;
  variant: WalkVariant;
  diff: {
    total_seconds_delta: number;
    distance_m_delta: number;
    cost_minor_delta: number;
    added_place_ids: number[];
    removed_place_ids: number[];
    prices_confirmed: boolean;
    text: string;
  };
};
