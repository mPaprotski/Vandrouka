import { parseApiBaseUrl } from '../config/api.ts';

export type ProbeAttempt = { id: string; browser_url: string; expires_at: string };
export type ProbeStatus = 'pending' | 'checking' | 'success' | 'error' | 'cancelled';

/** Isolated transport for the compatibility probe, never a session API. */
export function createBrowserProbe(origin: string, fetcher: typeof fetch = fetch) {
  const base = parseApiBaseUrl(origin);
  async function request(path: string, body: unknown) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 20000);
    try {
      const response = await fetcher(base + path, {
        method: 'POST', signal: controller.signal,
        headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
        body: JSON.stringify(body),
      });
      if (response.status === 503) throw new Error('Провайдер ещё не настроен на сервере проверки.');
      if (response.status === 401) throw new Error('Попытка завершена или срок её действия истёк. Начните заново.');
      if (!response.ok) throw new Error('Сервер проверки отклонил запрос. Повторите попытку.');
      return await response.json();
    } finally { clearTimeout(timer); }
  }
  return {
    async start(provider: 'google' | 'yandex', challenge: string): Promise<ProbeAttempt> {
      const value = await request('/oauth/attempts', { provider, challenge });
      if (!value || typeof value.id !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(value.id) ||
          typeof value.browser_url !== 'string' || value.browser_url !== `${base}/oauth/begin/${value.id}` ||
          typeof value.expires_at !== 'string' || !Number.isFinite(Date.parse(value.expires_at))) {
        throw new Error('Некорректный ответ сервера проверки.');
      }
      return value;
    },
    async result(id: string, secret: string): Promise<ProbeStatus> {
      const value = await request('/oauth/result', { id, secret });
      if (!value || !['pending', 'checking', 'success', 'error', 'cancelled'].includes(value.status)) {
        throw new Error('Некорректный результат проверки.');
      }
      return value.status;
    },
  };
}
