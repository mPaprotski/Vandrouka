import type { ReactNode } from 'react';
import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import { AppButton } from './AppButton';
import { colors } from './theme';

function StatePanel({ title, description, children, error = false }: {
  title: string; description?: string; children?: ReactNode; error?: boolean;
}) {
  return (
    <View style={styles.panel}>
      <Text accessibilityRole={error ? 'alert' : 'header'} style={[styles.title, error && styles.error]}>{title}</Text>
      {description ? <Text style={styles.description}>{description}</Text> : null}
      {children}
    </View>
  );
}

export function LoadingState({ message = 'Загружаем места…' }: { message?: string }) {
  return (
    <View accessibilityRole="progressbar" accessibilityLabel={message} accessibilityState={{ busy: true }} style={styles.panel}>
      <ActivityIndicator size="large" color={colors.primary} accessible={false} />
      <Text style={styles.description}>{message}</Text>
    </View>
  );
}

export function ErrorState({
  title = 'Не удалось загрузить данные',
  message = 'Проверьте подключение к интернету и попробуйте ещё раз.',
  onRetry, retrying = false, requestId,
}: {
  title?: string; message?: string; onRetry: () => void;
  retrying?: boolean; requestId?: string;
}) {
  return (
    <StatePanel title={title} description={message} error>
      <AppButton title={retrying ? 'Повторяем…' : 'Повторить'} onPress={onRetry} loading={retrying} />
      {requestId ? <Text selectable style={styles.reference}>Код обращения: {requestId}</Text> : null}
    </StatePanel>
  );
}

export function EmptyState({ title, description, action }: {
  title: string; description: string; action?: ReactNode;
}) {
  return <StatePanel title={title} description={description}>{action}</StatePanel>;
}

const styles = StyleSheet.create({
  panel: { backgroundColor: colors.surface, borderColor: colors.border, borderWidth: 1, borderRadius: 20, padding: 22, gap: 16 },
  title: { color: colors.primary, fontSize: 20, fontWeight: '600' },
  description: { color: colors.secondary, fontSize: 16, lineHeight: 24 },
  error: { color: colors.error },
  reference: { color: colors.secondary, fontSize: 12, lineHeight: 18 },
});
