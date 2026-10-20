import assert from 'node:assert/strict';
import test from 'node:test';
import { locateOnce, readFix } from '../src/location/locate.ts';

const position = () => ({ coords: { latitude: 53.9, longitude: 27.56, accuracy: 12 }, timestamp: Date.now() });
const granted = { granted: true, canAskAgain: true };
function setup(overrides = {}) {
  let callback, removed = 0, watched = 0;
  const provider = {
    hasServicesEnabledAsync: async () => true,
    getForegroundPermissionsAsync: async () => granted,
    requestForegroundPermissionsAsync: async () => granted,
    watch: async fn => { watched++; callback = fn; return { remove: () => { removed++; } }; },
    ...overrides,
  };
  return { provider, emit: value => callback(value), get removed() { return removed; }, get watched() { return watched; } };
}
const tick = () => new Promise(resolve => setImmediate(resolve));

test('fresh fix stops watcher and discards stale data', async () => {
  const mock = setup(), controller = new AbortController();
  const pending = locateOnce(mock.provider, controller.signal);
  await tick();
  const old = position(); old.timestamp -= 60000; mock.emit(old);
  const fresh = position(); mock.emit(fresh);
  assert.deepEqual(await pending, { ...fresh.coords, timestamp: fresh.timestamp });
  assert.equal(mock.removed, 1);
});
test('permission denied/blocked and disabled services never start GPS watcher', async () => {
  for (const [grantedPermission, code] of [[{granted:false,canAskAgain:true}, 'denied'], [{granted:false,canAskAgain:false}, 'blocked']]) {
    const mock = setup({ getForegroundPermissionsAsync: async () => grantedPermission, requestForegroundPermissionsAsync: async () => grantedPermission });
    await assert.rejects(locateOnce(mock.provider, new AbortController().signal), error => error.code === code);
    assert.equal(mock.watched, 0);
  }
  const disabled = setup({ hasServicesEnabledAsync: async () => false });
  await assert.rejects(locateOnce(disabled.provider, new AbortController().signal), error => error.code === 'disabled');
  assert.equal(disabled.watched, 0);
});
test('blur/background cancellation removes watcher and ignores late coordinates', async () => {
  const mock = setup(), controller = new AbortController();
  const pending = locateOnce(mock.provider, controller.signal);
  await tick(); controller.abort();
  await assert.rejects(pending, error => error.code === 'cancelled');
  assert.equal(mock.removed, 1); mock.emit(position());
});
test('timeout removes watcher', async () => {
  const mock = setup();
  await assert.rejects(locateOnce(mock.provider, new AbortController().signal, 15), error => error.code === 'timeout');
  assert.equal(mock.removed, 1);
});
test('late watcher registration is removed after cancellation', async () => {
  let register, removed = 0;
  const mock = setup({ watch: () => new Promise(resolve => { register = () => resolve({ remove: () => { removed++; } }); }) });
  const controller = new AbortController();
  const pending = locateOnce(mock.provider, controller.signal);
  await tick(); controller.abort();
  await assert.rejects(pending, error => error.code === 'cancelled');
  register(); await tick(); assert.equal(removed, 1);
});
test('invalid/stale/future fixes rejected; unknown accuracy allowed', () => {
  const now = Date.now();
  for (const value of [{ ...position(), timestamp: now - 30001 }, { ...position(), timestamp: now + 5001 }, { coords: { latitude: 100, longitude: 0, accuracy: 1 }, timestamp: now }, { coords: { latitude: 0, longitude: NaN, accuracy: 1 }, timestamp: now }]) assert.equal(readFix(value, now), null);
  assert.equal(readFix({ coords: { latitude: 0, longitude: 0, accuracy: null }, timestamp: now }, now).accuracy, null);
});

test('already aborted signal does not call native provider', async () => {
  const controller = new AbortController(); controller.abort();
  const mock = setup({ hasServicesEnabledAsync: async () => assert.fail('must not access provider') });
  await assert.rejects(locateOnce(mock.provider, controller.signal), error => error.code === 'cancelled');
  assert.equal(mock.watched, 0);
});

test('immediate first position cleans subscription exactly once', async () => {
  let removed = 0;
  const mock = setup({ watch: async callback => {
    callback(position()); return { remove: () => { removed++; } };
  } });
  await locateOnce(mock.provider, new AbortController().signal);
  await tick(); assert.equal(removed, 1);
});
