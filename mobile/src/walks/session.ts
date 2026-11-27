import { decodeWalkResult } from './decode.ts';
import type { VisitOutcome } from '../api/client.ts';
import type { WalkInput, WalkVariant } from './types.ts';

export type SavedWalk = {
  version: 1; revision: number; id: string; origin: string; owner: number;
  request: WalkInput; variant: WalkVariant; timezone: string;
  status: 'active' | 'paused' | 'finished'; startedAt: number; pausedAt: number | null;
  pausedMs: number; finishedAt: number | null; visits: Record<string, number>;
};
export type WalkFiles = { read(slot: number): Promise<string | null>; write(slot: number, data: string): Promise<void> };

export function decodeSavedWalk(raw: string, origin: string, owner: number): SavedWalk {
  const value = JSON.parse(raw) as SavedWalk;
  if (value?.version !== 1 || value.origin !== origin || value.owner !== owner ||
    typeof value.id !== 'string' || !/^[a-f0-9-]{36}$/i.test(value.id) || !Number.isSafeInteger(value.revision) || value.revision < 1 ||
    !['active','paused','finished'].includes(value.status) || !Number.isFinite(value.startedAt) || value.startedAt < 0 ||
    !Number.isFinite(value.pausedMs) || value.pausedMs < 0 ||
    (value.status === 'paused' ? !Number.isFinite(value.pausedAt) : value.pausedAt !== null) ||
    (value.status === 'finished' ? !Number.isFinite(value.finishedAt) : value.finishedAt !== null)) throw Error('Повреждена сохранённая прогулка.');
  const req = value.request;
  if (!req || typeof req.city !== 'string' || !req.start || !Number.isFinite(req.start.lat) || Math.abs(req.start.lat) > 90 ||
    !Number.isFinite(req.start.lon) || Math.abs(req.start.lon) > 180 || !Number.isFinite(Date.parse(req.start_at)) ||
    !Number.isSafeInteger(req.budget_minutes) || req.budget_minutes < 1 || req.budget_minutes>1440 || !Array.isArray(req.categories) ||
    req.categories.some(c=>typeof c!=='string') || !['slow','normal','fast'].includes(req.pace) ||
    (req.travel_mode!==undefined && !['walking','cycling'].includes(req.travel_mode)) ||
    (req.max_stops!==undefined && (!Number.isSafeInteger(req.max_stops)||req.max_stops<1||req.max_stops>100)) ||
    (req.max_distance_m!==undefined && (!Number.isFinite(req.max_distance_m)||req.max_distance_m<=0||req.max_distance_m>1000000))) throw Error('Повреждены параметры прогулки.');
  const decoded = decodeWalkResult({ seed: req.seed ?? 42, timezone: value.timezone, variants: [value.variant] });
  if (!value.visits || typeof value.visits !== 'object' || Array.isArray(value.visits) ||
    Object.entries(value.visits).some(([id, visit]) => !decoded.variants[0].stops.some(s => String(s.place_id) === id) || !Number.isSafeInteger(visit) || visit < 1)) throw Error('Повреждён прогресс прогулки.');
  return { ...value, variant: decoded.variants[0] };
}

export function createWalkStore(origin: string, owner: number, files: WalkFiles) {
  let queue = Promise.resolve<unknown>(undefined);
  let closed = false;
  function serial<T>(work: () => Promise<T>): Promise<T> {
    const result = queue.catch(() => {}).then(() => { if (closed) throw Error('Хранилище прогулки закрыто.'); return work(); }); queue = result; return result;
  }
  async function read(): Promise<SavedWalk | null> {
    const entries = await Promise.all([files.read(0), files.read(1)]);
    const valid: SavedWalk[] = [];
    for (const raw of entries) {
      if (raw !== null) { try { valid.push(decodeSavedWalk(raw, origin, owner)); } catch { /* The other journal slot may hold the last committed record. */ } }
    }
    if (!valid.length && entries.some(raw => raw !== null)) throw Error('Не удалось восстановить прогулку. Повторите чтение хранилища.');
    return valid.sort((a,b) => b.revision - a.revision)[0] ?? null;
  }
  async function write(value: SavedWalk) {
    // Alternate slots. A partial new write leaves the previous complete revision intact.
    const data = JSON.stringify(value);
    decodeSavedWalk(data, origin, owner);
    await files.write(value.revision % 2, data);
    return value;
  }
  async function update(id: string, operation: (walk: SavedWalk) => SavedWalk): Promise<SavedWalk> {
    const old = await read();
    if (!old || old.id !== id) throw Error('Эта прогулка больше не активна.');
    return write({ ...operation(old), revision: old.revision + 1 });
  }
  return {
    close: async () => { closed = true; await queue.catch(() => {}); },
    read: () => serial(read),
    start: (id: string, request: WalkInput, variant: WalkVariant, timezone: string, now: number) => serial(async () => {
      const old = await read();
      if (old && old.status !== 'finished') throw Error('Сначала завершите текущую прогулку.');
      return write({ version: 1, revision: (old?.revision ?? 0) + 1, id, origin, owner, request, variant, timezone,
        status: 'active', startedAt: now, pausedAt: null, pausedMs: 0, finishedAt: null, visits: {} });
    }),
    pause: (id: string, now: number) => serial(() => update(id, walk => walk.status === 'active' ? { ...walk, status: 'paused', pausedAt: now } : walk)),
    resume: (id: string, now: number) => serial(() => update(id, walk => walk.status === 'paused' ? { ...walk, status: 'active', pausedAt: null, pausedMs: walk.pausedMs + Math.max(0, now - walk.pausedAt!) } : walk)),
    finish: (id: string, now: number) => serial(() => update(id, walk => walk.status === 'finished' ? walk : { ...walk, status: 'finished', finishedAt: now,
      pausedMs: walk.pausedMs + (walk.pausedAt === null ? 0 : Math.max(0, now - walk.pausedAt)), pausedAt: null })),
    confirm: (id: string, outcome: VisitOutcome) => serial(async () => {
      const old = await read();
      // A replay for a completed older walk must never modify its replacement.
      if (!old || old.id !== id || outcome.visit.status !== 'confirmed' || !old.variant.stops.some(s => s.place_id === outcome.visit.place_id)) return;
      if (old.visits[String(outcome.visit.place_id)] !== undefined) return;
      await write({ ...old, revision: old.revision + 1, visits: { ...old.visits, [outcome.visit.place_id]: outcome.visit.id } });
    }),
  };
}

export function elapsedWalk(walk: SavedWalk, now: number): number {
  return Math.max(0, (walk.finishedAt ?? walk.pausedAt ?? now) - walk.startedAt - walk.pausedMs);
}
