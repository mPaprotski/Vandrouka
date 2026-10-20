import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { MAP_DOCUMENT_ORIGIN, isMapDocumentNavigation, getMapboxToken, mapDocument, parseMapMessage, placesScript, userLocationScript } from '../src/map/document.ts';

const points = [{ id: 9, name: '</script><img onerror=alert(1)>', lon: 27.56, lat: 53.9 }];

test('bridge validates schema, size and membership in catalog', () => {
  assert.deepEqual(parseMapMessage('{"type":"select","id":9}', points), { type: 'select', id: 9 });
  for (const raw of ['bad json', 'null', '{"type":"select","id":10}', '{"type":"select","id":"9"}', '{"type":"open","url":"https://bad"}', 'x'.repeat(1025)]) assert.equal(parseMapMessage(raw, points), null);
  assert.deepEqual(parseMapMessage('{"type":"ready"}', []), { type: 'ready' });
  assert.deepEqual(parseMapMessage('{"type":"error","code":"webgl"}', []), { type: 'error', code: 'webgl' });
  assert.equal(parseMapMessage('{"type":"error","code":"raw private token"}', []), null);
  assert.deepEqual(parseMapMessage('{"type":"progress","stage":"style"}', []), { type: 'progress', stage: 'style' });
  assert.equal(parseMapMessage('{"type":"progress","stage":"private"}', []), null);
});

test('public credential only, no secret tokens embedded', () => {
  for (const token of ['', 'sk.secret', 'pk.bad<script>']) assert.throws(() => mapDocument(token));
  const previous = process.env.EXPO_PUBLIC_MAPBOX_ACCESS_TOKEN;
  try {
    process.env.EXPO_PUBLIC_MAPBOX_ACCESS_TOKEN = 'sk.secret'; assert.equal(getMapboxToken(), null);
    process.env.EXPO_PUBLIC_MAPBOX_ACCESS_TOKEN = 'pk.test'; assert.equal(getMapboxToken(), 'pk.test');
  } finally {
    if (previous === undefined) delete process.env.EXPO_PUBLIC_MAPBOX_ACCESS_TOKEN;
    else process.env.EXPO_PUBLIC_MAPBOX_ACCESS_TOKEN = previous;
  }
});

test('marker data is safe script JSON with no executable HTML', () => {
  const script = placesScript(points);
  assert.ok(!script.includes('</script>'));
  let received;
  vm.runInNewContext(script, { window: { setPlaces: value => { received = value; } } });
  assert.equal(received[0].name, points[0].name);
  assert.deepEqual([received[0].lon, received[0].lat], [27.56, 53.9]);
});

test('document boot, marker click, refresh and empty set work without Mapbox/network', () => {
  const messages = [], events = {}, markers = [], elements = [];
  let options;
  class Map {
    constructor(value) { options = value; }
    on(name, callback) { events[name] = callback; }
    addControl() {}
    resize() {}
    remove() {}
    flyTo(value) { this.camera = value; }
  }
  class Marker {
    constructor({ element }) { this.element = element; markers.push(this); }
    setLngLat(value) { this.coordinates = value; return this; }
    addTo() { return this; }
    remove() { this.removed = true; }
  }
  const window = { ReactNativeWebView: { postMessage: value => messages.push(JSON.parse(value)) }, addEventListener() {} };
  const context = vm.createContext({ window, document: { createElement: () => {
    const element = { style: {}, setAttribute() {}, addEventListener: (name, fn) => { element[name] = fn; } };
    elements.push(element); return element;
  } }, mapboxgl: { supported: () => true, Map, Marker, NavigationControl: class {} } });
  window.mapboxgl = context.mapboxgl;
  const html = mapDocument('pk.test');
  const inline = html.match(/<script>([\s\S]*?)<\/script>/)[1];
  vm.runInContext(inline, context);
  window.startMap(); events['style.load']();
  assert.deepEqual(messages.splice(0), [{ type: 'progress', stage: 'document' }, { type: 'progress', stage: 'library' }, { type: 'progress', stage: 'style' }, { type: 'ready' }]);
  window.reportMapStatus();
  assert.deepEqual(messages.shift(), { type: 'ready' });
  assert.deepEqual(Array.from(options.center), [27.5615, 53.9023]);
  window.reportTileStatus();
  assert.deepEqual(messages.shift(), { type: 'tile-status', rendered: false, sourceRequested: false, sourceLoaded: false });
  events.render(); events.sourcedataloading(); events.sourcedata({ isSourceLoaded: true });
  window.reportTileStatus();
  assert.deepEqual(messages.shift(), { type: 'tile-status', rendered: true, sourceRequested: true, sourceLoaded: true });
  assert.equal(options.attributionControl, true);
  assert.equal(options.style, 'mapbox://styles/mpaprotski911/cmvfoqaqv00fl01s60qij53my');
  vm.runInContext(placesScript(points), context);
  assert.deepEqual(Array.from(markers[0].coordinates), [27.56, 53.9]);
  assert.equal(elements[0].textContent, '●');
  elements[0].click({ stopPropagation() {} });
  assert.deepEqual(messages.shift(), { type: 'select', id: 9 });
  vm.runInContext(placesScript([]), context);
  assert.equal(markers[0].removed, true);
  events.load();
  assert.deepEqual(messages.shift(), { type: 'tiles' });
  vm.runInContext(userLocationScript({ latitude: 53.9, longitude: 27.56, accuracy: 10, timestamp: 1 }), context);
  assert.deepEqual(Array.from(markers.at(-1).coordinates), [27.56, 53.9]);
  vm.runInContext(userLocationScript(null), context);
  assert.equal(markers.at(-1).removed, true);
  events.error({ error: { status: 401, message: 'secret' } });
  assert.deepEqual(messages.shift(), { type: 'error', code: 'auth' });
});

test('virtual HTML origin allowed without permitting navigation to remote pages', () => {
  assert.equal(isMapDocumentNavigation(MAP_DOCUMENT_ORIGIN), true);
  assert.equal(isMapDocumentNavigation('about:blank'), true);
  for (const url of ['https://app.vandrouka.local/other', 'https://app.vandrouka.local.evil/', 'https://api.mapbox.com/', 'javascript:alert(1)', 'file:///tmp/page']) {
    assert.equal(isMapDocumentNavigation(url), false);
  }
});
