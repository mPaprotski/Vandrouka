import * as Crypto from 'expo-crypto';
import * as SecureStore from 'expo-secure-store';
import * as Linking from 'expo-linking';
import { api } from '../api/client';
import { getApiBaseUrl } from '../config/api';
import type { SessionController } from './session';
import type { OAuthProvider } from './oauth-api';

type Pending = {
  version: 1; origin: string; id: string; secret: string; expires: string;
  purpose: 'login' | 'link' | 'delete'; owner?: number; target: string;
};
const key = 'vandrouka.oauth.pending.v1';
const options = { keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY };
let flight: Promise<Pending> | undefined;

export async function readOAuthAttempt(): Promise<Pending> {
  const raw = await SecureStore.getItemAsync(key, options);
  const a = raw ? JSON.parse(raw) as Pending : null;
  if (!a || a.version !== 1 || a.origin !== getApiBaseUrl() ||
      !/^[A-Za-z0-9_-]{43}$/.test(a.id) || !/^[0-9a-f]{64}$/.test(a.secret) ||
      !['login', 'link', 'delete'].includes(a.purpose) || !Number.isFinite(Date.parse(a.expires)) ||
      Date.parse(a.expires) <= Date.now() || !['/profile','/quests'].includes(a.target) && !/^\/capture\/[1-9]\d*$/.test(a.target)) {
    throw new Error('Попытка входа устарела. Начните заново.');
  }
  return a;
}
export async function cancelOAuth() { await SecureStore.deleteItemAsync(key, options); }

export async function beginOAuth(provider: OAuthProvider, purpose: Pending['purpose'], session: SessionController, owner?: number, password = '', target = '/profile') {
  const bytes = await Crypto.getRandomBytesAsync(32);
  const secret = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
  const challenge = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, secret);
  const a = purpose === 'login'
    ? await api.oauth.start(provider, challenge)
    : await session.withAccess(token => api.oauth.startAccount(provider, challenge, purpose, password, token), owner);
  const pending: Pending = { version: 1, origin: getApiBaseUrl(), id: a.attempt_id, secret, expires: a.expires_at, purpose, owner, target };
  await SecureStore.setItemAsync(key, JSON.stringify(pending), options);
  try { await Linking.openURL(a.browser_url); } catch (error) { await cancelOAuth(); throw error; }
}
export function finishOAuth(id: string, code: string, session: SessionController): Promise<Pending> {
  if (flight) return flight;
  const work = (async () => {
    const a = await readOAuthAttempt();
    if (a.id !== id || !/^[A-Za-z0-9_-]{43}$/.test(code)) throw new Error('Ссылка относится к другой попытке входа.');
    if (a.purpose === 'login') {
      const state = session.getSnapshot();
      if (state.status === 'authenticated') throw new Error('Вы уже вошли в аккаунт. Начните новый вход после выхода.');
      await session.signInWith(() => api.oauth.complete(id, code, a.secret));
    } else {
      await session.withAccess(token => api.oauth.completeAccount(id, code, a.secret, a.purpose, token), a.owner);
    }
    await cancelOAuth();
    return a;
  })();
  flight = work;
  return work.finally(() => { if (flight === work) flight = undefined; });
}
