import { useFocusEffect } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';
import type { QuestAccess } from './api';

export type QuestScope = {
  owner: number;
  withAccess: <T>(request: (auth: QuestAccess) => Promise<T>, signal?: AbortSignal) => Promise<T>;
};

type ReadState<T> = { data?: T; error?: unknown; busy: boolean };
const pollMs = 6500;
const maxDelayMs = 30000;
const neverPoll = () => false;

/** Only the visible foreground screen can poll. A failed poll keeps its last good result. */
export function useQuestRead<T>(
  load: (signal: AbortSignal) => Promise<T>,
  poll: (data: T) => boolean = neverPoll,
) {
  const [state, setState] = useState<ReadState<T>>({ busy: true });
  const start = useRef<() => void>(() => {});
  useFocusEffect(useCallback(() => {
    let focused = true;
    let foreground = AppState.currentState === 'active';
    let controller: AbortController | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let retryDelay = pollMs;
    let queued = false;
    let previous: T | undefined;
    const cancelTimer = () => clearTimeout(timer);
    const tick = () => {
      cancelTimer();
      if (!focused || !foreground) return;
      if (controller) { queued = true; return; }
      const request = new AbortController();
      controller = request;
      setState(old => ({ ...old, busy: true }));
      void (async () => {
        let delay: number | undefined;
        try {
          const data = await load(request.signal);
          if (request.signal.aborted) return;
          previous = data;
          setState({ data, busy: false });
          retryDelay = pollMs;
          if (poll(data)) delay = pollMs;
        } catch (error) {
          if (request.signal.aborted) return;
          setState(old => ({ ...old, busy: false, error }));
          if (previous === undefined ? poll !== neverPoll : poll(previous)) {
            retryDelay = Math.min(retryDelay * 2, maxDelayMs);
            delay = retryDelay;
          }
        } finally {
          if (controller === request) controller = undefined;
          if (queued) { queued = false; tick(); }
          else if (delay !== undefined && focused && foreground) timer = setTimeout(tick, delay);
        }
      })();
    };
    start.current = tick;
    const subscription = AppState.addEventListener('change', next => {
      const visible = next === 'active';
      if (visible === foreground) return;
      foreground = visible;
      if (visible) tick();
      else { queued = false; cancelTimer(); controller?.abort(); }
    });
    tick();
    return () => {
      focused = false;
      queued = false;
      start.current = () => {};
      cancelTimer();
      controller?.abort();
      subscription.remove();
    };
  }, [load, poll]));

  const refresh = useCallback(() => start.current(), []);
  return { ...state, refresh };
}

export function useQuestAction() {
  const current = useRef<AbortController | null>(null);
  const [state, setState] = useState<{ busy: boolean; error?: unknown }>({ busy: false });
  useEffect(() => () => current.current?.abort(), []);
  const run = async (action: (signal: AbortSignal) => Promise<void>) => {
    if (current.current) return;
    const controller = new AbortController();
    current.current = controller;
    setState({ busy: true });
    try {
      await action(controller.signal);
      if (!controller.signal.aborted) setState({ busy: false });
    } catch (error) {
      if (!controller.signal.aborted) setState({ error, busy: false });
    } finally {
      current.current = null;
    }
  };
  return { ...state, run };
}
