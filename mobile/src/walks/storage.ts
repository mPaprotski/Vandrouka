import * as FileSystem from 'expo-file-system/legacy';
import * as Crypto from 'expo-crypto';
import { createWalkStore } from './session';
import { getApiBaseUrl } from '../config/api';

const stores = new Map<string, ReturnType<typeof createWalkStore>>();
async function directory(origin: string, owner: number) {
  if (!FileSystem.documentDirectory) throw Error('Хранилище прогулок недоступно.');
  const hash = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, origin);
  return `${FileSystem.documentDirectory}walks-v1-${hash}-${owner}/`;
}
export function walkStorage(owner: number) {
  const origin = getApiBaseUrl(), key = `${origin}:${owner}`;
  const existing = stores.get(key);
  if (existing) return existing;
  const folder = directory(origin, owner);
  const store = createWalkStore(origin, owner, {
    async read(slot) {
      const path = `${await folder}${slot}.json`;
      const info = await FileSystem.getInfoAsync(path);
      if (!info.exists) return null;
      if ('size' in info && info.size > 16 * 1024 * 1024) throw Error('Сохранённый маршрут слишком большой.');
      return FileSystem.readAsStringAsync(path);
    },
    async write(slot, data) {
      const path = await folder;
      await FileSystem.makeDirectoryAsync(path, { intermediates: true });
      await FileSystem.writeAsStringAsync(`${path}${slot}.json`, data);
    },
  });
  stores.set(key, store);
  return store;
}
export async function clearWalks(origin: string, owner: number) {
  await stores.get(`${origin}:${owner}`)?.close();
  await FileSystem.deleteAsync(await directory(origin, owner), { idempotent: true });
  stores.delete(`${origin}:${owner}`);
}
