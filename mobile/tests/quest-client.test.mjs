import test from 'node:test';
import assert from 'node:assert/strict';
import { ApiError, createApiClient } from '../src/api/client.ts';

const participation = 'b40f2bee-b60d-4cb8-b5a4-21c2dc9295fd';
const attempt = '4da8529f-021e-4d6b-a15b-6b47a7017333';
const task = 'f03616ad-2763-4d3d-b493-cd9a0ba826a4';
const run = 'd11b8a14-e6f8-4e43-9cd4-400a2def1bb2';
const accessToken = 'x'.repeat(43);
const time = '2026-11-23T09:00:00Z';

const pending = {
  id: attempt, participation_id: participation, task_id: task, position: 1,
  revision: 3, status: 'pending_review', photo_ready: true, uploading: false,
  xp_awarded: 0, submitted_at: time,
};
function json(value, status = 200, headers = {}) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

test('a lost quest submission response can be recovered with the original key and GPS', async () => {
  const calls = [];
  const client = createApiClient({ baseUrl: 'https://example.com', fetcher: async (url, options) => {
    calls.push({ url, request: options.body, key: options.headers['Idempotency-Key'] });
    if (calls.length === 1) throw new Error('network dropped after dispatch');
    return json(pending);
  } });
  const submission = {
    key: '76a314a3-f32f-4e36-990a-5dba7fba2380',
    location: { lat: 53.9, lon: 27.55, accuracy_m: 10, observed_at: time },
  };
  await assert.rejects(client.quests.submit(attempt, submission, { accessToken }), { kind: 'network' });
  assert.equal((await client.quests.submit(attempt, submission, { accessToken })).status, 'pending_review');
  assert.deepEqual(calls[0], calls[1]);
  assert.equal(calls[1].key, submission.key);
});

test('expired review can be displayed without a usable photo or an active review', async () => {
  const client = createApiClient({ baseUrl: 'https://example.com', fetcher: async () => json({
    id: participation, run_id: run, title: 'Маршрут', status: 'active', joined_at: time,
    completed_steps: 0, total_steps: 2, stories: [], xp_awarded: 0, review_pending: false,
    current_task: { task_id: task, position: 1, clue: 'Подсказка', photo_task: 'Снимите вход' },
    current_attempt: { ...pending, status: 'expired', photo_ready: false },
  }) });
  const progress = await client.quests.progress(participation, { accessToken });
  assert.equal(progress.current_attempt.status, 'expired');
  assert.equal(progress.current_attempt.photo_ready, false);
  assert.equal(progress.review_pending, false);
});

test('quest upload throttling exposes the retry delay without a second request', async () => {
  let calls = 0;
  const client = createApiClient({ baseUrl: 'https://example.com', fetcher: async () => {
    calls++;
    return json({ error: { code: 'rate_limited', message: 'Обработка занята' } }, 429, { 'Retry-After': '2' });
  } });
  const file = new Blob(['photo'], { type: 'image/jpeg' });
  await assert.rejects(client.quests.upload(attempt, {
    uri: 'file:///fixture.jpg', file, size: file.size, width: 8, height: 8,
  }, { accessToken }), err => err instanceof ApiError && err.status === 429 && err.retryAfterMs === 2000);
  assert.equal(calls, 1);
});
