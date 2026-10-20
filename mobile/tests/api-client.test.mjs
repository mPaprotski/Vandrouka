import assert from 'node:assert/strict';
import test from 'node:test';
import { ApiError, createApiClient } from '../src/api/client.ts';

const origin = 'http://api.test:8080';
const sample = { id: 7, slug: 'town-hall', city: 'minsk', name: 'Ратуша', summary: 'История ратуши', lat: 53.9, lon: 27.56, categories: ['architecture'] };
const detail = { ...sample, visit_minutes: 30, hours_known: false, price_known: true, verified_at: '2026-10-08', opening_hours: [], opening_exceptions: [], prices: [{kind: 'free', amount: 0, currency: 'BYN'}], links: [] };
const json = (value, status = 200, headers = {}) => new Response(JSON.stringify(value), { status, headers });
const client = fetcher => createApiClient({ baseUrl: origin, fetcher });
const kind = expected => error => error instanceof ApiError && error.kind === expected;

test('catalog contract, URLs, filters and all five methods', async () => {
  const seen = [];
  const api = client(async (url, init) => {
    seen.push(url);
    assert.equal(init.headers.Accept, 'application/json');
    assert.ok(init.signal instanceof AbortSignal);
    if (url.endsWith('/health')) return json({ status: 'ok' });
    if (url.endsWith('/cities')) return json({ cities: [{ id: 1, slug: 'minsk', name: 'Минск', center_lat: 53.9, center_lon: 27.56, timezone: 'Europe/Minsk' }] });
    if (url.endsWith('/categories')) return json({ categories: [{ slug: 'architecture', name: 'Архитектура', places_count: 1 }] });
    if (url.endsWith('/places/7')) return json(detail);
    return json({ places: [sample] });
  });
  assert.deepEqual(await api.health(), { status: 'ok' });
  assert.equal((await api.listCities())[0].name, 'Минск');
  assert.equal((await api.listCategories('minsk'))[0].places_count, 1);
  assert.deepEqual(await api.listPlaces('minsk', { category: 'architecture', query: 'Ратуша & музей' }), [sample]);
  assert.deepEqual(await api.getPlace('7'), detail);
  assert.deepEqual(seen.slice(0, 3), [origin + '/health', origin + '/v1/cities', origin + '/v1/cities/minsk/categories']);
  const url = new URL(seen[3]);
  assert.equal(url.pathname, '/v1/cities/minsk/places');
  assert.equal(url.searchParams.get('q'), 'Ратуша & музей');
  assert.equal(url.searchParams.get('category'), 'architecture');
  assert.equal(seen[4], origin + '/v1/places/7');
});

test('empty catalog is valid; malformed data never becomes an empty success', async () => {
  assert.deepEqual(await client(async () => json({ places: [] })).listPlaces('minsk'), []);
  for (const body of [{}, { places: null }, { places: [null] }, { places: [{ ...sample, id: '7' }] }, { places: [{ ...sample, lat: 100 }] }, { places: [{ ...sample, categories: ['ok', 5] }] }]) {
    await assert.rejects(client(async () => json(body)).listPlaces('minsk'), kind('invalid_response'));
  }
  await assert.rejects(client(async () => json({ ...detail, id: 8 })).getPlace('7'), kind('invalid_response'));
});

test('HTTP APIError preserves status, server code and request ID', async () => {
  const api = client(async () => json({ error: { code: 'not_found', message: 'Объект не найден' } }, 404, { 'X-Request-ID': 'request-123' }));
  await assert.rejects(api.getPlace('7'), error => {
    assert.equal(error.kind, 'http'); assert.equal(error.status, 404);
    assert.equal(error.code, 'not_found'); assert.equal(error.message, 'Объект не найден');
    assert.equal(error.requestId, 'request-123'); return true;
  });
});

test('HTML errors, bad success JSON and network errors have safe messages', async () => {
  await assert.rejects(client(async () => new Response('<html>private upstream</html>', { status: 502 })).health(), error => error.kind === 'http' && !error.message.includes('private'));
  await assert.rejects(client(async () => new Response('not json', { headers: { 'X-Request-ID': 'bad-json' } })).health(), error => error.kind === 'invalid_response' && error.requestId === 'bad-json');
  await assert.rejects(client(async () => { throw new Error('private network details'); }).health(), error => error.kind === 'network' && !error.message.includes('private'));
  await assert.rejects(client(async () => json({ status: 'unavailable' }, 503)).health(), error => error.kind === 'http' && error.status === 503);
});

test('timeout aborts fetch even when transport ignores cancellation', async () => {
  let signal;
  const api = createApiClient({ baseUrl: origin, timeoutMs: 10, fetcher: async (_url, init) => { signal = init.signal; return new Promise(() => {}); } });
  await assert.rejects(api.health(), kind('timeout'));
  assert.equal(signal.aborted, true);
});

test('timeout also covers reading the response body', async () => {
  const api = createApiClient({ baseUrl: origin, timeoutMs: 10, fetcher: async () => ({ ok: true, status: 200, headers: new Headers({ 'X-Request-ID': 'slow-body' }), json: () => new Promise(() => {}) }) });
  await assert.rejects(api.health(), error => error.kind === 'timeout' && error.requestId === 'slow-body');
});

test('external abort and already cancelled request', async () => {
  const controller = new AbortController();
  let called = 0;
  const api = client(async () => { called++; return new Promise(() => {}); });
  const pending = api.health({ signal: controller.signal });
  controller.abort();
  await assert.rejects(pending, kind('cancelled'));
  const previous = called;
  await assert.rejects(api.health({ signal: controller.signal }), kind('cancelled'));
  assert.equal(called, previous);
});

test('invalid IDs and slugs do not reach transport', async () => {
  const api = client(async () => assert.fail('must not fetch'));
  for (const id of ['town-hall', '-1', '0', '7/path', '9007199254740992']) await assert.rejects(api.getPlace(id), kind('invalid_request'));
  assert.throws(() => api.listPlaces('../private'), kind('invalid_request'));
  assert.throws(() => api.listCategories(''), kind('invalid_request'));
});

test('details decoder preserves breaks, closed exceptions, unknown flags and sources', async () => {
  const input = { ...detail, hours_known: true, price_known: false,
    opening_hours: [{ weekday: 1, open_time: '09:00', close_time: '12:00' }, { weekday: 1, open_time: '14:00', close_time: '18:00' }],
    opening_exceptions: [{ date: '2026-12-25', closed: true, open_time: null, close_time: null, note: 'Праздник' }],
    links: [{ kind: 'official', url: 'https://example.com', title: 'Источник' }],
  };
  assert.deepEqual(await client(async () => json(input)).getPlace('7'), input);
  for (const bad of [{ ...input, visit_minutes: -1 }, { ...input, hours_known: 'yes' }, { ...input, opening_hours: [{ weekday: 7, open_time: '09:00', close_time: '18:00' }] }, { ...input, prices: [{kind:'adult',amount:-1,currency:'BYN'}] }]) {
    await assert.rejects(client(async () => json(bad)).getPlace('7'), kind('invalid_response'));
  }
});

const curated = { order: 1, story: 'История места', approach: 'Внешний осмотр', sources: ['https://example.com/history'], reviewed_on: '2026-10-20', field_verified: false, cover: { url: '/catalog-media/town-hall.jpg?v=abcdef123456', author: 'Author', license: 'CC BY-SA 4.0', license_url: 'https://creativecommons.org/licenses/by-sa/4.0/', source_url: 'https://commons.wikimedia.org/wiki/File:Example.jpg', changes: 'Без изменений' } };
test('curated content survives decoding; unsafe cover paths and sources fail closed', async () => {
  const input = { ...detail, content: curated };
  assert.deepEqual(await client(async () => json(input)).getPlace('7'), input);
  for (const changed of [
    { ...curated, cover: { ...curated.cover, url: '/catalog-media/../../secret' } },
    { ...curated, cover: { ...curated.cover, url: 'https://other.test/photo.jpg' } },
    { ...curated, cover: { ...curated.cover, license_url: 'javascript:alert(1)' } },
    { ...curated, sources: ['not a URL'] },
    { ...curated, field_verified: 'yes' },
  ]) await assert.rejects(client(async () => json({ ...input, content: changed })).getPlace('7'), kind('invalid_response'));
});
