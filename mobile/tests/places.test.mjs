import assert from 'node:assert/strict';
import test from 'node:test';
import { demoPlaces, navigationUrls, safeSourceUrl, ticketLines, weeklyHours } from '../src/places/presentation.ts';

test('weekly schedule orders days, preserves lunch breaks and date rollover', () => {
  const input = [{ weekday: 1, open_time: '14:00', close_time: '18:00' }, { weekday: 1, open_time: '09:00', close_time: '12:00' }, { weekday: 0, open_time: '22:00', close_time: '02:00' }];
  const lines = weeklyHours(input);
  assert.equal(lines[0], 'Пн: 09:00–12:00, 14:00–18:00');
  assert.equal(lines[1], 'Вт: закрыто');
  assert.equal(lines[6], 'Вс: 22:00–02:00 (следующий день)');
  assert.equal(input[0].open_time, '14:00');
});
test('unknown tickets are never displayed as free; known zero remains free', () => {
  assert.match(ticketLines({ price_known: false, prices: [{ kind: 'free', amount: 0, currency: 'BYN' }] })[0], /неизвестна/);
  assert.match(ticketLines({ price_known: true, prices: [] })[0], /неизвестна/);
  assert.equal(ticketLines({ price_known: true, prices: [{ kind: 'adult', amount: 15.5, currency: 'BYN' }] })[0], 'Взрослый: 15,50 BYN');
  assert.equal(ticketLines({ price_known: true, prices: [{ kind: 'free', amount: 0, currency: 'BYN' }] })[0], 'Вход: бесплатно');
});
test('navigation preserves coordinates and encodes labels without URL injection', () => {
  const urls = navigationUrls({ lat: 53.9, lon: 27.56, name: 'Ратуша & музей' }, 'android');
  assert.equal(urls.length, 2);
  assert.equal(decodeURIComponent(urls[0]), 'geo:0,0?q=53.9,27.56(Ратуша & музей)');
  assert.equal(new URL(urls[1]).searchParams.get('destination'), '53.9,27.56');
  assert.equal(new URL(urls[1]).searchParams.get('travelmode'), 'walking');
  assert.deepEqual(navigationUrls({ lat: 91, lon: 0, name: 'Invalid' }, 'android'), []);
  for (const url of ['javascript:alert(1)', 'file:///secret', 'https://user:secret@host', 'not a URL']) assert.equal(safeSourceUrl(url), null);
});

test('demo selection preserves editorial order without altering the full catalog', () => {
  const all = [{ id: 1 }, { id: 2, content: { order: 2 } }, { id: 3, content: { order: 1 } }];
  assert.deepEqual(demoPlaces(all).map(p => p.id), [3, 2]);
  assert.deepEqual(all.map(p => p.id), [1, 2, 3]);
  assert.deepEqual(demoPlaces([{ id: 1 }]), []);
});
