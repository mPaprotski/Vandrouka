import type { OpeningHours, PlaceDetails } from '../api/client.ts';

export function weeklyHours(hours: readonly OpeningHours[]): string[] {
  const names = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб'];
  return [1, 2, 3, 4, 5, 6, 0].map(day => {
    const intervals = hours.filter(h => h.weekday === day).sort((a, b) => a.open_time.localeCompare(b.open_time));
    return `${names[day]}: ${intervals.length ? intervals.map(h => `${h.open_time}–${h.close_time}${h.close_time < h.open_time ? ' (следующий день)' : ''}`).join(', ') : 'закрыто'}`;
  });
}

export function ticketLines(place: Pick<PlaceDetails, 'price_known' | 'prices'>): string[] {
  if (!place.price_known || place.prices.length === 0) return ['Стоимость неизвестна — уточните перед посещением.'];
  const labels: Record<string, string> = { adult: 'Взрослый', discount: 'Льготный', free: 'Вход' };
  return place.prices.map(price => `${labels[price.kind] ?? price.kind}: ${price.amount === 0 ? 'бесплатно' : `${price.amount.toFixed(2).replace('.', ',')} ${price.currency}`}`);
}

export function safeSourceUrl(value: string): string | null {
  try {
    const url = new URL(value);
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : null;
  } catch { return null; }
}

export function navigationUrls(place: { lat: number; lon: number; name: string }, platform: string): string[] {
  if (!Number.isFinite(place.lat) || Math.abs(place.lat) > 90 || !Number.isFinite(place.lon) || Math.abs(place.lon) > 180) return [];
  const coordinates = `${place.lat},${place.lon}`;
  const web = `https://www.google.com/maps/dir/?api=1&destination=${encodeURIComponent(coordinates)}&travelmode=walking`;
  if (platform === 'android') return [`geo:0,0?q=${encodeURIComponent(`${coordinates}(${place.name})`)}`, web];
  if (platform === 'ios') return [`https://maps.apple.com/?daddr=${encodeURIComponent(coordinates)}&dirflg=w`, web];
  return [web];
}

/** Curated demo order is editorial; never reorder the caller's full catalog. */
export function demoPlaces<T extends { content?: { order: number } }>(places: readonly T[]): T[] {
  return places.filter(place => place.content).sort((a, b) => a.content!.order - b.content!.order);
}
