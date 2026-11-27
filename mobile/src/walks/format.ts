import type { WalkTotals } from './types.ts';

export function duration(seconds: number): string {
  const minutes = Math.ceil(seconds / 60);
  return minutes < 60 ? `${minutes} мин` : `${Math.floor(minutes / 60)} ч${minutes % 60 ? ` ${minutes % 60} мин` : ''}`;
}
export function distance(meters: number): string {
  return meters < 1000 ? `${Math.round(meters)} м` : `${(meters / 1000).toLocaleString('ru-RU', { maximumFractionDigits: 1 })} км`;
}
export function ticketCost(totals: WalkTotals): string {
  const amount = `${(totals.cost_minor / 100).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${totals.currency}`;
  return totals.cost_complete ? `Билеты — ${amount}` : `Известная стоимость билетов — ${amount}. Стоимость известна не для всех мест.`;
}
export function walkTime(iso: string, timezone: string): string {
  return new Intl.DateTimeFormat('ru-RU', { timeZone: timezone, day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(iso));
}
