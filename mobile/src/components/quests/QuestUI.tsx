import { router } from 'expo-router';
import { Fragment, useMemo } from 'react';
import type { ReactNode } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import type { TextInputProps } from 'react-native';
import { ApiError } from '../../api/errors';
import { useSession } from '../../auth/SessionProvider';
import type { QuestScope } from '../../quests/requests';
import { SectionScreen } from '../SectionScreen';
import { AppButton } from '../ui/AppButton';
import { ErrorState, LoadingState } from '../ui/ContentState';
import { colors } from '../ui/theme';

export function QuestScreen({ title, description, children }: { title: string; description?: string; children?: ReactNode }) {
  return <SectionScreen title={title} description={description} city={null}>{children}</SectionScreen>;
}

export function QuestGate({ title, children }: { title: string; children: (scope: QuestScope) => ReactNode }) {
  const { state, controller } = useSession();
  const owner = state.status === 'authenticated' ? state.user.id : undefined;
  const scope = useMemo<QuestScope | undefined>(() => owner === undefined ? undefined : {
    owner,
    withAccess: (work, signal) => controller.withAccess(token => work({ accessToken: token, signal }), owner),
  }, [controller, owner]);

  if (scope) return <Fragment key={scope.owner}>{children(scope)}</Fragment>;
  return <QuestScreen title={title}>
    {state.status === 'loading' && <LoadingState message="Проверяем вход…" />}
    {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => void controller.resume()} />}
    {state.status === 'guest' && <>
      <Text style={questStyles.text}>Войдите, чтобы создать квест или присоединиться к прогулке.</Text>
      <AppButton title="Войти" onPress={() => router.push({ pathname: '/auth', params: { returnTo: 'quests' } })} />
    </>}
  </QuestScreen>;
}

export function QuestPanel({ title, children }: { title?: string; children?: ReactNode }) {
  return <View style={questStyles.panel}>
    {title && <Text accessibilityRole="header" style={questStyles.heading}>{title}</Text>}
    {children}
  </View>;
}

export function QuestField({ label, ...input }: TextInputProps & { label: string }) {
  return <View style={questStyles.field}>
    <Text style={questStyles.label}>{label}</Text>
    <TextInput {...input} accessibilityLabel={label} placeholderTextColor={colors.secondary}
      style={[questStyles.input, input.multiline && questStyles.multiline]} />
  </View>;
}

export function QuestError({ error }: { error: unknown }) {
  if (error == null) return null;
  const message = error instanceof Error ? error.message : 'Не удалось выполнить действие. Попробуйте ещё раз.';
  return <View accessibilityLiveRegion="polite">
    <Text accessibilityRole="alert" style={questStyles.error}>{message}</Text>
    {error instanceof ApiError && error.requestId ? <Text selectable style={questStyles.note}>Код обращения {error.requestId}</Text> : null}
  </View>;
}

export function QuestLoad({ error, retry }: { error?: unknown; retry: () => void }) {
  if (error == null) return <LoadingState message="Загружаем данные квеста…" />;
  return <ErrorState message={error instanceof Error ? error.message : undefined}
    requestId={error instanceof ApiError ? error.requestId : undefined} onRetry={retry} />;
}

export const questStyles = StyleSheet.create({
  panel: { padding: 18, gap: 12, borderRadius: 18, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.surface },
  heading: { color: colors.primary, fontSize: 21, fontWeight: '700' },
  text: { color: colors.primary, fontSize: 16, lineHeight: 24 },
  note: { color: colors.secondary, fontSize: 14, lineHeight: 21 },
  field: { gap: 7 },
  label: { color: colors.primary, fontWeight: '600', fontSize: 15 },
  input: { color: colors.primary, fontSize: 16, minHeight: 54, padding: 14, borderRadius: 14, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.surface },
  multiline: { minHeight: 98, textAlignVertical: 'top' },
  error: { color: colors.error, fontSize: 15, lineHeight: 22 },
  code: { color: colors.primary, fontWeight: '700', fontSize: 27, letterSpacing: 3, textAlign: 'center' },
  photo: { width: '100%', height: 290, borderRadius: 15, overflow: 'hidden', backgroundColor: '#17231e' },
});
