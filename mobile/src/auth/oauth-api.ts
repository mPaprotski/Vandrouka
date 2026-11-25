import { decodeAuthSession } from '../api/client.ts';
import type { ApiRequest } from '../api/client.ts';

export type OAuthProvider = 'google' | 'yandex';
export type OAuthAttempt = { attempt_id: string; browser_url: string; expires_at: string };

export function createOAuthAPI(request: ApiRequest, origin: string) {
  const decodeAttempt = (value: unknown) => {
    const a = value as OAuthAttempt;
    if (!a || typeof a.attempt_id !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(a.attempt_id) ||
      a.browser_url !== `${origin}/v1/auth/oauth/begin/${a.attempt_id}` ||
      !Number.isFinite(Date.parse(a.expires_at))) throw new Error('Некорректная попытка входа');
    return a;
  };
  return {
    methods(token: string) {
      return request('/v1/me/login-methods', value => {
        const m=value as {password:boolean;providers:OAuthProvider[]};
        if (!m || typeof m.password!=='boolean' || !Array.isArray(m.providers) || m.providers.some(p=>!['google','yandex'].includes(p))) throw new Error('Некорректные способы входа');
        return m;
      }, {}, {accessToken:token});
    },
    startAccount(provider:OAuthProvider,challenge:string,purpose:string,password:string,token:string) {
      return request('/v1/auth/oauth/account-attempts',decodeAttempt,{}, {method:'POST',body:{provider,challenge,purpose,password},accessToken:token});
    },
    completeAccount(attempt_id:string,code:string,device_secret:string,purpose:string,token:string) {
      return request('/v1/auth/oauth/account-complete',()=>undefined,{}, {method:'POST',body:{attempt_id,code,device_secret,purpose},accessToken:token});
    },
    start(provider: OAuthProvider, challenge: string) {
      return request('/v1/auth/oauth/attempts', value => {
        const a = value as OAuthAttempt;
        if (!a || typeof a.attempt_id !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(a.attempt_id) ||
            a.browser_url !== `${origin}/v1/auth/oauth/begin/${a.attempt_id}` ||
            typeof a.expires_at !== 'string' || !Number.isFinite(Date.parse(a.expires_at))) {
          throw new Error('Некорректный ответ сервера входа.');
        }
        return a;
      }, {}, { method: 'POST', body: { provider, challenge } });
    },
    complete(attempt_id: string, code: string, device_secret: string) {
      return request('/v1/auth/oauth/complete', decodeAuthSession, {}, {
        method: 'POST', body: { attempt_id, code, device_secret },
      });
    },
  };
}
