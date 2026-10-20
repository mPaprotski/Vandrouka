export type PositionFix = { latitude: number; longitude: number; accuracy: number | null; timestamp: number };
type NativePosition = { coords: { latitude: number; longitude: number; accuracy: number | null }; timestamp: number };
type Subscription = { remove(): void };
export type LocationProvider = {
  hasServicesEnabledAsync(): Promise<boolean>;
  getForegroundPermissionsAsync(): Promise<{ granted: boolean; canAskAgain: boolean }>;
  requestForegroundPermissionsAsync(): Promise<{ granted: boolean; canAskAgain: boolean }>;
  watch(callback: (position: NativePosition) => void, onError: () => void): Promise<Subscription>;
};
export class LocationError extends Error {
  readonly code: 'denied' | 'blocked' | 'disabled' | 'timeout' | 'cancelled' | 'unavailable';
  constructor(code: LocationError['code'], message: string) { super(message); this.name = 'LocationError'; this.code = code; }
}

export function readFix(position: NativePosition, now = Date.now()): PositionFix | null {
  const { latitude, longitude, accuracy } = position.coords;
  if (!Number.isFinite(latitude) || Math.abs(latitude) > 90 || !Number.isFinite(longitude) || Math.abs(longitude) > 180 || !Number.isFinite(position.timestamp) || now - position.timestamp > 30000 || position.timestamp - now > 5000 || (accuracy !== null && (!Number.isFinite(accuracy) || accuracy < 0))) return null;
  return { latitude, longitude, accuracy, timestamp: position.timestamp };
}

/** A single fresh fix. Stop the native watcher on success, timeout, blur or background. */
export async function locateOnce(provider: LocationProvider, signal: AbortSignal, timeoutMs = 20000, accept: (fix: PositionFix) => boolean = () => true): Promise<PositionFix> {
  let subscription: Subscription | undefined;
  const stop = () => { const active = subscription; subscription = undefined; active?.remove(); };
  let finished = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let onAbort = () => {};
  try {
    return await new Promise<PositionFix>((resolve, reject) => {
      const fail = (error: LocationError) => { if (!finished) { finished = true; reject(error); } };
      onAbort = () => fail(new LocationError('cancelled', 'Определение положения отменено.'));
      if (signal.aborted) { onAbort(); return; }
      signal.addEventListener('abort', onAbort);
      timer = setTimeout(() => fail(new LocationError('timeout', 'Не удалось определить положение вовремя. Попробуйте на открытом месте.')), timeoutMs);
      void (async () => {
        if (!await provider.hasServicesEnabledAsync()) throw new LocationError('disabled', 'Геолокация выключена. Включите её в настройках телефона.');
        if (finished) return;
        let permission = await provider.getForegroundPermissionsAsync();
        if (finished) return;
        if (!permission.granted && permission.canAskAgain) permission = await provider.requestForegroundPermissionsAsync();
        if (finished) return;
        if (!permission.granted) throw new LocationError(permission.canAskAgain ? 'denied' : 'blocked', 'Доступ к геолокации не разрешён. Карта и список мест по-прежнему доступны.');
        subscription = await provider.watch(position => {
          if (finished) return;
          const fix = readFix(position);
          if (fix && accept(fix)) { finished = true; resolve(fix); }
        }, () => fail(new LocationError('unavailable', 'Не удалось определить положение. Попробуйте ещё раз.')));
        if (finished) stop();
      })().catch(error => fail(error instanceof LocationError ? error : new LocationError('unavailable', 'Не удалось определить положение. Попробуйте ещё раз.')));
    });
  } finally {
    finished = true;
    if (timer) clearTimeout(timer);
    signal.removeEventListener('abort', onAbort);
    stop();
  }
}
