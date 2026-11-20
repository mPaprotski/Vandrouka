import * as Crypto from 'expo-crypto';
import { useRef, useState } from 'react';
import { Linking, Text } from 'react-native';
import { createBrowserProbe } from '../auth/browser-probe';
import type { ProbeAttempt } from '../auth/browser-probe';
import { SectionScreen, sectionStyles } from './SectionScreen';
import { AppButton } from './ui/AppButton';

export function BrowserAuthProbe({ origin }: { origin: string }) {
  const [message, setMessage] = useState('Выберите провайдера для проверки.');
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState(false);
  const flight = useRef(false);
  const attempt = useRef<{ value: ProbeAttempt; secret: string } | null>(null);

  async function start(provider: 'google' | 'yandex') {
    if (flight.current) return;
    flight.current = true; setBusy(true);
    attempt.current = null; setPending(false);
    try {
      const bytes = await Crypto.getRandomBytesAsync(32);
      const secret = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
      const challenge = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, secret);
      const value = await createBrowserProbe(origin).start(provider, challenge);
      attempt.current = { value, secret }; setPending(true);
      await Linking.openURL(value.browser_url);
      setMessage('Завершите проверку в браузере, вернитесь сюда и нажмите «Проверить результат».');
    } catch (error) {
      attempt.current = null; setPending(false);
      setMessage(error instanceof Error ? error.message : 'Не удалось открыть проверку.');
    } finally { flight.current = false; setBusy(false); }
  }

  async function check() {
    if (flight.current || !attempt.current) return;
    flight.current = true; setBusy(true);
    try {
      const status = await createBrowserProbe(origin).result(attempt.current.value.id, attempt.current.secret);
      const messages = {
        pending: 'Ожидаем завершения в браузере.', checking: 'Провайдер проверяет вход. Повторите проверку чуть позже.',
        success: 'Переход через браузер и проверка провайдера работают. Создание аккаунта подключается следующим этапом.',
        cancelled: 'Вход отменён в браузере.', error: 'Провайдер не подтвердил вход. Проверьте настройки и повторите.',
      };
      setMessage(messages[status]);
      if (status !== 'pending' && status !== 'checking') { attempt.current = null; setPending(false); }
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Не удалось получить результат проверки.');
    } finally { flight.current = false; setBusy(false); }
  }

  return <SectionScreen title="Проверка входа через браузер" description="Диагностический шаг для Expo Go. Проверяет Google и Яндекс, создание аккаунта выполняется отдельным этапом.">
    <Text accessibilityLiveRegion="polite" style={sectionStyles.note}>{message}</Text>
    <AppButton title="Проверить Google" disabled={busy} onPress={() => { void start('google'); }} />
    <AppButton title="Проверить Яндекс" disabled={busy} onPress={() => { void start('yandex'); }} />
    {pending && <AppButton title="Проверить результат" loading={busy} onPress={() => { void check(); }} />}
  </SectionScreen>;
}
