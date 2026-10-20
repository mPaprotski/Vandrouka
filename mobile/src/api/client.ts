import { getApiBaseUrl, parseApiBaseUrl } from '../config/api.ts';

export type City = { id: number; slug: string; name: string; center_lat: number; center_lon: number; timezone: string };
export type Category = { slug: string; name: string; places_count: number };
// Only fields consumed by the current mobile screens. Other server fields are ignored.
export type CatalogCover = { url: string; author: string; license: string; license_url: string; source_url: string; changes: string };
export type CuratedContent = { order: number; story: string; approach: string; sources: string[]; reviewed_on: string; field_verified: boolean; cover: CatalogCover };
export type Place = { content?: CuratedContent; id: number; slug: string; city: string; name: string; summary: string; lat: number; lon: number; categories: string[] };
export type OpeningHours = { weekday: number; open_time: string; close_time: string };
export type OpeningException = { date: string; closed: boolean; open_time: string | null; close_time: string | null; note: string };
export type TicketPrice = { kind: string; amount: number; currency: string };
export type SourceLink = { kind: string; url: string; title: string };
export type PlaceDetails = Place & {
  visit_minutes: number; hours_known: boolean; price_known: boolean; verified_at: string;
  opening_hours: OpeningHours[]; opening_exceptions: OpeningException[];
  prices: TicketPrice[]; links: SourceLink[];
};
export type AuthUser = {id: number; email: string; created_at: string};
export type AuthSession = {user: AuthUser; access_token: string; refresh_token: string; token_type: 'Bearer'; expires_at: string; refresh_expires_at: string};
export type Credentials = {email: string; password: string};
export type UploadedPhoto = {id:string; place_id:number; status:'ready'; mime_type:'image/jpeg'|'image/png'; size_bytes:number; width:number; height:number; created_at:string; ready_at:string};
export type LocalPhoto = {uri:string; width:number; height:number; size:number};
export type VisitRejection='too_far'|'poor_accuracy'|'stale_location'|'future_location'|'inactive_place'|'photo_not_ready';
export type VisitInput={photo_id:string;location:{lat:number;lon:number;accuracy_m:number;observed_at:string}};
export type VisitOutcome={visit:{id:number;place_id:number;photo_id:string;status:'confirmed'|'rejected';created_at:string;confirmed_at?:string;rejection_code?:VisitRejection};xp_awarded:0|100;error?:{code:VisitRejection;message:string}};
const visitReasons:VisitRejection[]=['too_far','poor_accuracy','stale_location','future_location','inactive_place','photo_not_ready'];
export function decodeVisitOutcome(value:unknown):VisitOutcome{
 const r=object(value),v=object(r.visit),xp=integer(r.xp_awarded,0),photo=named(v.photo_id);
 if(!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(photo)||(xp!==0&&xp!==100))return invalid();
 const base={id:integer(v.id),place_id:integer(v.place_id),photo_id:photo,created_at:timestamp(v.created_at)};
 if(v.status==='confirmed'&&v.rejection_code===undefined&&r.error===undefined)return {visit:{...base,status:'confirmed',confirmed_at:timestamp(v.confirmed_at)},xp_awarded:xp};
 if(v.status==='rejected'&&v.confirmed_at===undefined&&xp===0){
  const error=object(r.error),code=named(v.rejection_code) as VisitRejection;
  if(!visitReasons.includes(code)||error.code!==code)return invalid();
  return {visit:{...base,status:'rejected',rejection_code:code},xp_awarded:0,error:{code,message:named(error.message)}};
 }
 return invalid();
}
export type RequestOptions = { signal?: AbortSignal; timeoutMs?: number };
export type ErrorKind = 'http' | 'network' | 'timeout' | 'cancelled' | 'invalid_response' | 'invalid_request';

export class ApiError extends Error {
  readonly kind: ErrorKind;
  readonly status?: number;
  readonly code?: string;
  readonly requestId?: string;
  constructor(kind: ErrorKind, message: string, details: { status?: number; code?: string; requestId?: string } = {}) {
    super(message);
    this.name = 'ApiError';
    this.kind = kind;
    Object.assign(this, details);
  }
}

function invalid(): never { throw new ApiError('invalid_response', 'Сервер вернул данные в неожиданном формате.'); }
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return invalid();
  return value as Record<string, unknown>;
}
function text(value: unknown): string { return typeof value === 'string' ? value : invalid(); }
function named(value: unknown): string { const result = text(value); return result.trim() ? result : invalid(); }
function number(value: unknown): number { return typeof value === 'number' && Number.isFinite(value) ? value : invalid(); }
function integer(value: unknown, minimum = 1): number {
  const result = number(value); return Number.isSafeInteger(result) && result >= minimum ? result : invalid();
}
function coordinate(value: unknown, limit: number): number { const result = number(value); return Math.abs(result) <= limit ? result : invalid(); }
function array<T>(value: unknown, decode: (item: unknown) => T): T[] { return Array.isArray(value) ? value.map(decode) : invalid(); }
function city(value: unknown): City {
  const v = object(value);
  return { id: integer(v.id), slug: named(v.slug), name: named(v.name), center_lat: coordinate(v.center_lat, 90), center_lon: coordinate(v.center_lon, 180), timezone: named(v.timezone) };
}
function category(value: unknown): Category {
  const v = object(value); return { slug: named(v.slug), name: named(v.name), places_count: integer(v.places_count, 0) };
}
function content(value: unknown): CuratedContent {
  const c = object(value), cover = object(c.cover);
  const imageURL = named(cover.url);
  if (!/^\/catalog-media\/[a-z0-9-]+\.(jpg|png)\?v=[a-f0-9]{12}$/.test(imageURL)) return invalid();
  const https = (value: unknown) => { const result = named(value); try { const url = new URL(result); return url.protocol === 'https:' && !url.username && !url.password ? result : invalid(); } catch { return invalid(); } };
  return { order: integer(c.order), story: named(c.story), approach: named(c.approach), sources: array(c.sources, https), reviewed_on: named(c.reviewed_on), field_verified: boolean(c.field_verified),
    cover: { url: imageURL, author: named(cover.author), license: named(cover.license), license_url: https(cover.license_url), source_url: https(cover.source_url), changes: named(cover.changes) } };
}
function place(value: unknown): Place {
  const v = object(value);
  return { ...(v.content === undefined ? {} : { content: content(v.content) }), id: integer(v.id), slug: named(v.slug), city: named(v.city), name: named(v.name), summary: text(v.summary), lat: coordinate(v.lat, 90), lon: coordinate(v.lon, 180), categories: array(v.categories, named) };
}
function boolean(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function clock(value: unknown): string {
  const result = text(value); return /^(?:[01][0-9]|2[0-3]):[0-5][0-9]$|^24:00$/.test(result) ? result : invalid();
}
function details(value: unknown): PlaceDetails {
  const v = object(value);
  return { ...place(value), visit_minutes: integer(v.visit_minutes, 0), hours_known: boolean(v.hours_known), price_known: boolean(v.price_known), verified_at: text(v.verified_at),
    opening_hours: array(v.opening_hours, entry => {
      const h = object(entry), weekday = integer(h.weekday, 0);
      if (weekday > 6) return invalid();
      return { weekday, open_time: clock(h.open_time), close_time: clock(h.close_time) };
    }),
    opening_exceptions: array(v.opening_exceptions, entry => {
      const e = object(entry), date = text(e.date);
      if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(Date.parse(date))) return invalid();
      return { date, closed: boolean(e.closed), open_time: e.open_time === null ? null : clock(e.open_time), close_time: e.close_time === null ? null : clock(e.close_time), note: text(e.note) };
    }),
    prices: array(v.prices, entry => {
      const p = object(entry), amount = number(p.amount);
      if (amount < 0) return invalid();
      return { kind: named(p.kind), amount, currency: named(p.currency) };
    }),
    links: array(v.links, entry => { const l = object(entry); return { kind: named(l.kind), url: named(l.url), title: text(l.title) }; }),
  };
}
function slug(value: string): string {
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value) || value.length > 100) throw new ApiError('invalid_request', 'Некорректный идентификатор города или категории.');
  return value;
}

function timestamp(value: unknown): string {
 const result = named(value);return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(result) && Number.isFinite(Date.parse(result)) ? result : invalid();
}
export function decodeAuthUser(value: unknown): AuthUser {
 const user=object(value);return {id:integer(user.id),email:named(user.email),created_at:timestamp(user.created_at)};
}
export function decodeAuthSession(value: unknown): AuthSession {
 const session=object(value);
 const token=(value:unknown)=>{const result=named(value);return /^[A-Za-z0-9_-]{43}$/.test(result)?result:invalid();};
 if(session.token_type!=='Bearer')return invalid();
 const expires=timestamp(session.expires_at),refreshExpires=timestamp(session.refresh_expires_at);
 if(Date.parse(expires)>Date.parse(refreshExpires))return invalid();
 return {user:decodeAuthUser(session.user),access_token:token(session.access_token),refresh_token:token(session.refresh_token),token_type:'Bearer',expires_at:expires,refresh_expires_at:refreshExpires};
}
export function decodeUploadedPhoto(value:unknown):UploadedPhoto {
 const p=object(value),id=named(p.id),width=integer(p.width),height=integer(p.height),size=integer(p.size_bytes);
 if(!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(id)||p.status!=='ready'||(p.mime_type!=='image/jpeg'&&p.mime_type!=='image/png')||size>10*1024*1024||width>8192||height>8192||width*height>20000000)return invalid();
 return {id,place_id:integer(p.place_id),status:'ready',mime_type:p.mime_type,size_bytes:size,width,height,created_at:timestamp(p.created_at),ready_at:timestamp(p.ready_at)};
}
function credentials(input: Credentials): Credentials {
 const email=input.email.trim();const length=[...input.password].length;
 if(!email||email.length>254||length<12||length>128)throw new ApiError('invalid_request','Введите email и пароль длиной 12–128 символов.');
 return {email,password:input.password};
}
export function createApiClient(options: { baseUrl?: string; fetcher?: typeof fetch; timeoutMs?: number } = {}) {
  async function request<T>(path: string, decode: (value: unknown, status:number) => T, requestOptions: RequestOptions = {}, transport: {method?: 'POST'|'DELETE'; body?: unknown; form?:FormData; accessToken?: string; idempotencyKey?:string; acceptStatus?:number} = {}): Promise<T> {
    const base = options.baseUrl === undefined ? getApiBaseUrl() : parseApiBaseUrl(options.baseUrl);
    const timeoutMs = requestOptions.timeoutMs ?? options.timeoutMs ?? 10000;
    if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) throw new ApiError('invalid_request', 'Некорректное время ожидания запроса.');
    const controller = new AbortController();
    let timedOut = false;
    let requestId: string | undefined;
    let status: number | undefined;
    let rejectAbort: (error: ApiError) => void = () => {};
    const interrupted = new Promise<never>((_, reject) => { rejectAbort = reject; });
    const abort = () => {
      controller.abort();
      rejectAbort(new ApiError(timedOut ? 'timeout' : 'cancelled', timedOut ? 'Сервер не ответил вовремя. Попробуйте ещё раз.' : 'Запрос отменён.', { requestId, status }));
    };
    requestOptions.signal?.addEventListener('abort', abort);
    const timer = setTimeout(() => { timedOut = true; abort(); }, timeoutMs);
    const work = async () => {
      if (requestOptions.signal?.aborted) {
        throw new ApiError('cancelled', 'Запрос отменён.');
      }
      const headers: Record<string,string> = {Accept:'application/json'};
      if(transport.body!==undefined)headers['Content-Type']='application/json';
      if(transport.accessToken){if(!/^[A-Za-z0-9_-]{43}$/.test(transport.accessToken))throw new ApiError('invalid_request','Некорректная сессия.');headers.Authorization=`Bearer ${transport.accessToken}`;}
      if(transport.idempotencyKey)headers['Idempotency-Key']=transport.idempotencyKey;
      const response = await (options.fetcher ?? fetch)(base + path, {signal:controller.signal,headers,method:transport.method,body:transport.form??(transport.body===undefined?undefined:JSON.stringify(transport.body))});
      requestId = response.headers.get('X-Request-ID') ?? undefined;
      status = response.status;
      let data: unknown;
      try { data = response.status===204?undefined:await response.json(); } catch {
        if (response.ok) return invalid();
      }
      if (!response.ok && response.status!==transport.acceptStatus) {
        let code: string | undefined;
        let message = response.status === 404 ? 'Место или город не найдены.' : 'Не удалось получить данные с сервера. Попробуйте ещё раз.';
        if (data && typeof data === 'object' && 'error' in data) {
          const detail = (data as { error: unknown }).error;
          if (detail && typeof detail === 'object') {
            if ('code' in detail && typeof detail.code === 'string') code = detail.code;
            if ('message' in detail && typeof detail.message === 'string' && detail.message.trim()) message = detail.message;
          }
        }
        throw new ApiError('http', message, { status, code, requestId });
      }
      return decode(data,response.status);
    };
    try {
      return await Promise.race([work(), interrupted]);
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.kind === 'invalid_response') throw new ApiError(error.kind, error.message, { requestId, status });
        throw error;
      }
      throw new ApiError('network', 'Нет связи с сервером. Проверьте подключение и попробуйте ещё раз.', { requestId, status });
    } finally {
      clearTimeout(timer);
      requestOptions.signal?.removeEventListener('abort', abort);
    }
  }
  return {
    confirmVisit:(placeId:number,key:string,input:VisitInput,accessToken:string,o?:RequestOptions)=>{
      const uuid=/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i;
      const l=input.location;
      if(!Number.isSafeInteger(placeId)||placeId<1||!uuid.test(key)||!uuid.test(input.photo_id)||!Number.isFinite(l.lat)||Math.abs(l.lat)>90||!Number.isFinite(l.lon)||Math.abs(l.lon)>180||!Number.isFinite(l.accuracy_m)||l.accuracy_m<0||!Number.isFinite(Date.parse(l.observed_at)))throw new ApiError('invalid_request','Некорректные параметры посещения.');
      const body={photo_id:input.photo_id,location:{lat:l.lat,lon:l.lon,accuracy_m:l.accuracy_m,observed_at:l.observed_at}};
      return request(`/v1/places/${placeId}/visits`,(value,status)=>{const result=decodeVisitOutcome(value);if(![200,201,422].includes(status)||(status===422)!==(result.visit.status==='rejected'))return invalid();if(result.visit.place_id!==placeId||result.visit.photo_id.toLowerCase()!==input.photo_id.toLowerCase())return invalid();return result;},o,{method:'POST',body,accessToken,idempotencyKey:key,acceptStatus:422});
    },
    uploadPhoto:(placeId:number,photo:LocalPhoto,accessToken:string,o:RequestOptions={})=>{
      if(!Number.isSafeInteger(placeId)||placeId<1||!photo.uri.startsWith('file://')||!Number.isSafeInteger(photo.size)||photo.size<1||photo.size>10*1024*1024||!Number.isSafeInteger(photo.width)||!Number.isSafeInteger(photo.height)||photo.width<1||photo.height<1||photo.width>8192||photo.height>8192||photo.width*photo.height>20000000)throw new ApiError('invalid_request','Снимок недоступен или превышает допустимый размер. Сделайте другой.');
      const form=new FormData();form.append('place_id',String(placeId));
      // React Native's FormData reads the local file URI; it does not load base64 into JS.
      form.append('file',{uri:photo.uri,type:'image/jpeg',name:'visit.jpg'} as unknown as Blob);
      return request('/v1/visits/photo-upload',value=>{const result=decodeUploadedPhoto(value);if(result.place_id!==placeId)return invalid();return result;},{...o,timeoutMs:o.timeoutMs??60000},{method:'POST',form,accessToken});
    },
    discardPhoto:(photoId:string,accessToken:string,o?:RequestOptions)=>{
      if(!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(photoId))throw new ApiError('invalid_request','Некорректная фотография.');
      return request(`/v1/visits/photos/${photoId}?unconfirmed_only=true`,()=>undefined,o,{method:'DELETE',accessToken});
    },
    deletePhoto:(photoId:string,accessToken:string,o?:RequestOptions)=>{
      if(!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(photoId))throw new ApiError('invalid_request','Некорректная фотография.');
      return request(`/v1/visits/photos/${photoId}`,()=>undefined,o,{method:'DELETE',accessToken});
    },
    register: (input:Credentials,o?:RequestOptions)=>request('/v1/auth/register',decodeAuthSession,o,{method:'POST',body:credentials(input)}),
    login: (input:Credentials,o?:RequestOptions)=>request('/v1/auth/login',decodeAuthSession,o,{method:'POST',body:credentials(input)}),
    refresh: (refreshToken:string,o?:RequestOptions)=>request('/v1/auth/refresh',decodeAuthSession,o,{method:'POST',body:{refresh_token:refreshToken}}),
    logout: (refreshToken:string,o?:RequestOptions)=>request('/v1/auth/logout',()=>undefined,o,{method:'POST',body:{refresh_token:refreshToken}}),
    me: (accessToken:string,o?:RequestOptions)=>request('/v1/me',value=>decodeAuthUser(object(value).user),o,{accessToken}),
    health: (o?: RequestOptions) => request('/health', value => {
      if (object(value).status !== 'ok') return invalid();
      return { status: 'ok' as const };
    }, o),
    listCities: (o?: RequestOptions) => request('/v1/cities', value => array(object(value).cities, city), o),
    listCategories: (citySlug: string, o?: RequestOptions) => request(`/v1/cities/${slug(citySlug)}/categories`, value => array(object(value).categories, category), o),
    listPlaces: (citySlug: string, filters: { category?: string; query?: string } = {}, o?: RequestOptions) => {
      const query = new URLSearchParams();
      if (filters.category) query.set('category', slug(filters.category));
      if (filters.query) query.set('q', filters.query);
      const suffix = query.toString();
      return request(`/v1/cities/${slug(citySlug)}/places${suffix ? `?${suffix}` : ''}`, value => array(object(value).places, place), o);
    },
    getPlace: (id: string, o?: RequestOptions) => {
      if (!/^[1-9][0-9]*$/.test(id) || !Number.isSafeInteger(Number(id))) return Promise.reject(new ApiError('invalid_request', 'Некорректная ссылка на место.'));
      return request(`/v1/places/${id}`, value => {
        const result = details(value);
        if (result.id !== Number(id)) return invalid();
        return result;
      }, o);
    },
  };
}

export const api = createApiClient();
