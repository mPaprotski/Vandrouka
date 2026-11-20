import { router, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useRef, useState } from 'react';
import { KeyboardAvoidingView, Platform, StyleSheet, Text, TextInput, View } from 'react-native';
import { ApiError } from '../api/client';
import { useSession } from '../auth/SessionProvider';
import { SectionScreen } from './SectionScreen';
import { AppButton } from './ui/AppButton';
import { LoadingState } from './ui/ContentState';
import { PasswordInput } from './ui/PasswordInput';
import { colors } from './ui/theme';

export function AuthForm({ mode }: { mode: 'login' | 'register' }) {
  const { state, controller } = useSession();
  const { placeId } = useLocalSearchParams<{ placeId?: string }>();
  const captureId = typeof placeId === 'string' && /^[1-9]\d*$/.test(placeId) && Number.isSafeInteger(Number(placeId)) ? placeId : undefined;
  const registering = mode === 'register';
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState<string>();
  const [requestId, setRequestId] = useState<string>();
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const unavailable = busy || state.status === 'loading' || state.status === 'authenticated';

  useFocusEffect(useCallback(() => {
    if (state.status === 'authenticated') {
      if (captureId) router.replace({ pathname: '/capture/[id]', params: { id: captureId } });
      else router.replace('/profile');
    }
  }, [state.status, captureId]));

  async function submit() {
    if (submitting.current || unavailable) return;
    setError(undefined);
    setRequestId(undefined);
    if (!email.trim() || [...password].length < 12 || [...password].length > 128) {
      setError('Введите email и пароль длиной 12–128 символов.');
      return;
    }
    if (registering && password !== confirmation) {
      setError('Пароли не совпадают.');
      return;
    }
    submitting.current = true;
    setBusy(true);
    try {
      await controller.signIn(mode, { email, password });
      setPassword('');
      setConfirmation('');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : registering ? 'Не удалось создать аккаунт. Повторите попытку.' : 'Не удалось войти. Повторите попытку.');
      setRequestId(cause instanceof ApiError ? cause.requestId : undefined);
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }

  function switchScreen() {
    setPassword('');
    setConfirmation('');
    router.replace({ pathname: registering ? '/auth' : '/register', params: captureId ? { placeId: captureId } : {} });
  }

  function continueAsGuest() {
    if (captureId) router.replace({ pathname: '/places/[id]', params: { id: captureId } });
    else router.replace('/explore');
  }

  return (
    <KeyboardAvoidingView style={styles.screen} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <SectionScreen
        title={registering ? 'Создайте аккаунт' : 'С возвращением'}
        description={registering ? 'Сохраняйте фотографии, открывайте места и собирайте свою историю прогулок.' : 'Войдите, чтобы продолжить свою историю открытий.'}
      >
        {state.status === 'loading' || state.status === 'authenticated' ? <LoadingState message="Проверяем вход…" /> : (
          <View style={styles.form}>
            <Text style={styles.label}>Электронная почта</Text>
            <TextInput
              accessibilityLabel="Электронная почта" value={email} onChangeText={setEmail}
              editable={!unavailable} keyboardType="email-address" autoCapitalize="none" autoCorrect={false}
              autoComplete="email" textContentType="emailAddress" maxLength={254}
              placeholder="name@example.com" placeholderTextColor={colors.secondary} style={styles.input}
            />
            <Text style={styles.label}>Пароль</Text>
            <PasswordInput label="Пароль" value={password} onChangeText={setPassword} editable={!unavailable} newPassword={registering} />
            <Text style={styles.note}>От 12 до 128 символов. Пробелы в пароле учитываются.</Text>
            {registering && <>
              <Text style={styles.label}>Повторите пароль</Text>
              <PasswordInput label="Повторите пароль" value={confirmation} onChangeText={setConfirmation} editable={!unavailable} newPassword />
            </>}
            {error && <Text accessibilityRole="alert" style={styles.error}>{error}</Text>}
            {requestId && <Text selectable style={styles.note}>Код обращения — {requestId}</Text>}
            <AppButton title={registering ? 'Создать аккаунт' : 'Войти'} loading={busy} disabled={unavailable} onPress={() => { void submit(); }} />
            <AppButton title={registering ? 'У меня уже есть аккаунт' : 'Создать аккаунт'} variant="secondary" disabled={unavailable} onPress={switchScreen} />
            {__DEV__ && process.env.EXPO_PUBLIC_OAUTH_PROBE_URL && <AppButton title="Проверить Google и Яндекс" variant="secondary" disabled={unavailable} onPress={() => router.push('/auth-check')} />}
            <AppButton title="Продолжить без входа" variant="secondary" disabled={unavailable} onPress={continueAsGuest} />
          </View>
        )}
      </SectionScreen>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  form: { gap: 12 },
  label: { fontSize: 15, color: colors.primary, fontWeight: '600' },
  input: { backgroundColor: colors.surface, borderColor: colors.border, borderWidth: 1, borderRadius: 14, padding: 16, minHeight: 56, fontSize: 16, color: colors.primary },
  note: { color: colors.secondary, fontSize: 13, lineHeight: 20 },
  error: { color: colors.error, fontSize: 15, lineHeight: 22 },
});
