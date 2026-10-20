import { useFocusEffect } from 'expo-router';
import * as Location from 'expo-location';
import { useCallback, useRef, useState } from 'react';
import { AppState } from 'react-native';
import { locateOnce, LocationError, type LocationProvider, type PositionFix } from '../location/locate';

const provider: LocationProvider = {
  hasServicesEnabledAsync: Location.hasServicesEnabledAsync,
  getForegroundPermissionsAsync: Location.getForegroundPermissionsAsync,
  requestForegroundPermissionsAsync: Location.requestForegroundPermissionsAsync,
  watch: (callback, onError) => Location.watchPositionAsync({ accuracy: Location.Accuracy.Balanced, timeInterval: 1000, distanceInterval: 0 }, callback, onError),
};

type State = { status: 'idle' } | { status: 'loading' } | { status: 'success'; fix: PositionFix } | { status: 'error'; message: string; blocked: boolean };

export function useForegroundLocation() {
  const [state, setState] = useState<State>({ status: 'idle' });
  const current = useRef<AbortController | null>(null);
  useFocusEffect(useCallback(() => {
    const cancel = () => {
      current.current?.abort(); current.current = null;
      setState({ status: 'idle' });
    };
    const listener = AppState.addEventListener('change', next => { if (next !== 'active') cancel(); });
    return () => { listener.remove(); cancel(); };
  }, []));

  const locate = useCallback(async () => {
    if (current.current || AppState.currentState !== 'active') return;
    const controller = new AbortController(); current.current = controller;
    setState({ status: 'loading' });
    try {
      const fix = await locateOnce(provider, controller.signal);
      if (current.current === controller) setState({ status: 'success', fix });
    } catch (error) {
      if (current.current === controller && !controller.signal.aborted) setState({ status: 'error', message: error instanceof Error ? error.message : 'Не удалось определить положение.', blocked: error instanceof LocationError && error.code === 'blocked' });
    } finally {
      if (current.current === controller) current.current = null;
    }
  }, []);
  return { state, locate };
}
