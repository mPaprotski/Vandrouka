import * as Crypto from 'expo-crypto';
import * as FileSystem from 'expo-file-system/legacy';
import * as SecureStore from 'expo-secure-store';
import type { LocalPhoto } from '../api/client';
import { photoFile } from '../api/photoFile.native';
import { validateLocalPhoto } from '../visits/localPhoto';
import { isQuestID } from './decode';
import type { Submission } from './types';

type SavedPhoto = { name: string; width: number; height: number; size: number };
export type QuestDraft = { task: string; attempt?: string; photo?: SavedPhoto; submission?: Submission };
const options = { keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY };
const queues = new Map<string, Promise<unknown>>();
const maxDrafts = 40;

async function serial<T>(key: string, work: () => Promise<T>): Promise<T> {
  const next = (queues.get(key) ?? Promise.resolve()).catch(() => {}).then(work);
  queues.set(key, next);
  try { return await next; } finally { if (queues.get(key) === next) queues.delete(key); }
}
async function prefix(origin: string, owner: number) {
  const hash = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, origin);
  return `vandrouka.quest.v1.${hash}.${owner}`;
}
function folder(key: string) {
  if (!FileSystem.cacheDirectory) throw new Error('Хранилище снимков недоступно.');
  return `${FileSystem.cacheDirectory}${key}/`;
}
function corrupt(): never { throw new Error('Не удалось прочитать сохранённую попытку.'); }
function savedPhoto(value: unknown): value is SavedPhoto {
  const p = value as Partial<SavedPhoto> | undefined;
  return p !== undefined && p !== null && typeof p.name === 'string' && p.name.endsWith('.jpg') &&
    isQuestID(p.name.slice(0, -4)) && typeof p.width === 'number' && typeof p.height === 'number' &&
    Number.isSafeInteger(p.width) && p.width > 0 && Number.isSafeInteger(p.height) && p.height > 0 &&
    p.width * p.height <= 4_000_000 && typeof p.size === 'number' && Number.isSafeInteger(p.size) && p.size > 0 && p.size <= 10 * 1024 * 1024;
}
function savedSubmission(value: unknown): value is Submission {
  const s = value as Partial<Submission> | undefined;
  const fix = s?.location;
  return isQuestID(s?.key) && !!fix &&
    Number.isFinite(fix.lat) && Math.abs(fix.lat) <= 90 && Number.isFinite(fix.lon) && Math.abs(fix.lon) <= 180 &&
    Number.isFinite(fix.accuracy_m) && fix.accuracy_m >= 0 && typeof fix.observed_at === 'string' && Number.isFinite(Date.parse(fix.observed_at));
}
function decode(raw: string | null): QuestDraft | undefined {
  if (raw === null) return undefined;
  try {
    const data = JSON.parse(raw);
    if (data.version !== 1 || !isQuestID(data.task) || (data.attempt !== undefined && !isQuestID(data.attempt)) ||
        (data.photo !== undefined && !savedPhoto(data.photo)) ||
        (data.submission !== undefined && (!data.attempt || data.photo || !savedSubmission(data.submission)))) return corrupt();
    return { task: data.task, attempt: data.attempt, photo: data.photo, submission: data.submission };
  } catch { return corrupt(); }
}
async function readIndex(key: string): Promise<{ deleted: boolean; ids: string[] }> {
  const raw = await SecureStore.getItemAsync(key + '.index', options);
  if (raw === null) return { deleted: false, ids: [] };
  const data = JSON.parse(raw);
  if (typeof data.deleted !== 'boolean' || !Array.isArray(data.ids) || data.ids.length > maxDrafts || !data.ids.every(isQuestID)) return corrupt();
  return data;
}

export function questDraftStorage(origin: string, owner: number, participation: string) {
  if (!isQuestID(participation)) throw new Error('Некорректная ссылка на прохождение.');
  const scope = prefix(origin, owner);
  async function work<T>(fn: (key: string, fullKey: string) => Promise<T>) {
    const key = await scope;
    const fullKey = key + '.' + participation;
    return serial(key, () => fn(key, fullKey));
  }
  const saveIndex = (key: string, value: { deleted: boolean; ids: string[] }) =>
    SecureStore.setItemAsync(key + '.index', JSON.stringify(value), options);

  return {
    read: () => work(async (key, fullKey) => {
      if ((await readIndex(key)).deleted) throw new Error('Аккаунт удалён.');
      return decode(await SecureStore.getItemAsync(fullKey, options));
    }),
    write: (value: QuestDraft | undefined) => work(async (key, fullKey) => {
      const index = await readIndex(key);
      if (index.deleted) throw new Error('Аккаунт удалён.');
      if (value) {
        if (!index.ids.includes(participation)) {
          if (index.ids.length >= maxDrafts) throw new Error('Завершите один из сохранённых квестов перед новой попыткой.');
          index.ids.push(participation);
          await saveIndex(key, index);
        }
        const old = decode(await SecureStore.getItemAsync(fullKey, options));
        await SecureStore.setItemAsync(fullKey, JSON.stringify({ version: 1, ...value }), options);
        if (old?.photo && old.photo.name !== value.photo?.name) {
          await FileSystem.deleteAsync(folder(key) + old.photo.name, { idempotent: true }).catch(() => {});
        }
      } else {
        const old = decode(await SecureStore.getItemAsync(fullKey, options));
        if (old?.photo) await FileSystem.deleteAsync(folder(key) + old.photo.name, { idempotent: true });
        await SecureStore.deleteItemAsync(fullKey, options);
        if (index.ids.includes(participation)) {
          index.ids = index.ids.filter(id => id !== participation);
          await saveIndex(key, index);
        }
      }
    }),
    stage: (photo: LocalPhoto) => work(async key => {
      if ((await readIndex(key)).deleted) throw new Error('Аккаунт удалён.');
      if (photo.width * photo.height > 4_000_000) throw new Error('Снимок слишком большой для обработки. Сделайте другой.');
      const name = Crypto.randomUUID() + '.jpg';
      const location = folder(key);
      await FileSystem.makeDirectoryAsync(location, { intermediates: true });
      await FileSystem.moveAsync({ from: photo.uri, to: location + name });
      return { name, width: photo.width, height: photo.height, size: photo.size };
    }),
    async photo(value: SavedPhoto): Promise<LocalPhoto | undefined> {
      const uri = folder(await scope) + value.name;
      const info = await FileSystem.getInfoAsync(uri);
      if (!info.exists) return undefined;
      return { ...validateLocalPhoto({ ...value, uri, format: 'jpg' }, info), file: photoFile(uri) };
    },
    async forgetPhoto(value: SavedPhoto) {
      await FileSystem.deleteAsync(folder(await scope) + value.name, { idempotent: true });
    },
  };
}

export async function clearQuestDrafts(origin: string, owner: number) {
  const key = await prefix(origin, owner);
  await serial(key, async () => {
    const index = await readIndex(key);
    index.deleted = true;
    await SecureStore.setItemAsync(key + '.index', JSON.stringify(index), options);
    if (FileSystem.cacheDirectory) await FileSystem.deleteAsync(folder(key), { idempotent: true });
    for (const id of index.ids) await SecureStore.deleteItemAsync(key + '.' + id, options);
    await SecureStore.setItemAsync(key + '.index', JSON.stringify({ deleted: true, ids: [] }), options);
  });
}
