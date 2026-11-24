import { useState } from 'react';
import { Text } from 'react-native';
import { router } from 'expo-router';
import * as Crypto from 'expo-crypto';
import { useSession } from '../../auth/SessionProvider';
import { walkStorage } from '../../walks/storage';
import type { WalkInput, WalkVariant } from '../../walks/types';
import { AppButton } from '../ui/AppButton';

export function StartWalkButton({ request, variant, timezone }: { request: WalkInput; variant: WalkVariant; timezone: string }) {
  const { state } = useSession();
  const [busy, setBusy] = useState(false), [message, setMessage] = useState('');
  const [acceptSchedule, setAcceptSchedule] = useState(false);
  async function start() {
    if (busy || state.status !== 'authenticated') return;
    if (!acceptSchedule && Math.abs(Date.now() - Date.parse(request.start_at)) > 15 * 60 * 1000) {
      setAcceptSchedule(true); setMessage('Расписание рассчитано на другое время. Можно построить прогулку заново или начать с сохранённым расписанием.'); return;
    }
    setBusy(true); setMessage('');
    try {
      const store = walkStorage(state.user.id), old = await store.read();
      if (old && old.status !== 'finished') { router.push('/walks/active'); return; }
      await store.start(Crypto.randomUUID(), request, variant, timezone, Date.now());
      router.push('/walks/active');
    } catch (error) { setMessage(error instanceof Error ? error.message : 'Не удалось сохранить прогулку.'); }
    finally { setBusy(false); }
  }
  if (state.status !== 'authenticated') return <><Text>Войдите, чтобы начать прогулку и сохранять её прогресс.</Text><AppButton title="Войти" onPress={() => router.push('/auth')} /></>;
  return <>
    {message ? <Text accessibilityLiveRegion="polite">{message}</Text> : null}
    {acceptSchedule && <AppButton title="Перестроить на текущее время" variant="secondary" onPress={() => router.navigate('/walks')} />}
    <AppButton title={acceptSchedule ? 'Начать с сохранённым расписанием' : 'Начать прогулку'} disabled={variant.stops.length === 0} loading={busy} onPress={() => void start()} />
  </>;
}
