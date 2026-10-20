import { ActivityIndicator, Pressable, StyleSheet, Text } from 'react-native';
import { colors } from './theme';

export type AppButtonProps = {
  title: string;
  onPress: () => void;
  disabled?: boolean;
  loading?: boolean;
  accessibilityLabel?: string;
  expanded?: boolean;
  variant?: 'primary' | 'secondary';
};

export function AppButton({
  title, onPress, disabled = false, loading = false,
  accessibilityLabel, expanded, variant = 'primary',
}: AppButtonProps) {
  const unavailable = disabled || loading;
  const foreground = variant === 'primary' ? colors.background : colors.primary;

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? title}
      accessibilityState={{ disabled: unavailable, busy: loading, expanded }}
      disabled={unavailable}
      onPress={onPress}
      style={({ pressed }) => [
        styles.button,
        variant === 'secondary' && styles.secondary,
        unavailable && styles.disabled,
        pressed && styles.pressed,
      ]}
    >
      {loading && <ActivityIndicator color={foreground} accessible={false} />}
      <Text style={[styles.title, { color: foreground }]}>{title}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  button: { minHeight: 56, paddingHorizontal: 20, paddingVertical: 16, borderRadius: 16, backgroundColor: colors.primary, flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 12, borderWidth: 1, borderColor: colors.primary },
  secondary: { backgroundColor: colors.surface, borderColor: colors.border },
  title: { flexShrink: 1, textAlign: 'center', fontSize: 16, fontWeight: '600' },
  disabled: { opacity: 0.5 },
  pressed: { opacity: 0.8 },
});
