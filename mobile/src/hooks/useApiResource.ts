import { useFocusEffect } from 'expo-router';
import { useCallback, useState } from 'react';
import { ApiError } from '../api/client';

type State<T> = { status: 'loading' } | { status: 'success'; data: T } | { status: 'error'; message: string; requestId?: string };

/** Abort on blur, route change, retry and unmount; never publish stale responses. */
export function useApiResource<T>(load: (signal: AbortSignal) => Promise<T>) {
  const [state, setState] = useState<State<T>>({ status: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [data, setData] = useState<T>();
  const retry = useCallback(() => setAttempt(value => value + 1), []);
  useFocusEffect(useCallback(() => {
    const controller = new AbortController();
    setState({ status: 'loading' });
    void Promise.resolve().then(() => load(controller.signal)).then(data => {
      if (!controller.signal.aborted) { setData(data); setState({ status: 'success', data }); }
    }).catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setState({ status: 'error', message: error instanceof Error ? error.message : 'Не удалось загрузить данные.', requestId: error instanceof ApiError ? error.requestId : undefined });
    });
    return () => controller.abort();
  // eslint-disable-next-line react-hooks/exhaustive-deps -- Explicit retry counter restarts the focused request.
  }, [load, attempt]));
  return { state, retry, data };
}
