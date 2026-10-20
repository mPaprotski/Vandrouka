import type { ReactNode } from 'react';
import { ScrollView, StyleSheet, Text } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { colors } from './ui/theme';

export function SectionScreen({ title, description, children }: {
  title: string; description: string; children?: ReactNode;
}) {
  return (
    <SafeAreaView edges={['left', 'right']} style={styles.screen}>
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.content}>
        <Text style={styles.city}>МИНСК</Text>
        <Text accessibilityRole="header" style={styles.title}>{title}</Text>
        <Text style={styles.description}>{description}</Text>
        {children}
      </ScrollView>
    </SafeAreaView>
  );
}

export const sectionStyles = StyleSheet.create({
  note: { color: colors.secondary, fontSize: 14, lineHeight: 22, marginTop: 12 },
});

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  content: { padding: 26, gap: 14, maxWidth: 560, width: '100%', alignSelf: 'center' },
  city: { color: colors.accent, fontSize: 11, letterSpacing: 2 },
  title: { color: colors.primary, fontSize: 30, fontWeight: '700' },
  description: { color: colors.secondary, fontSize: 16, lineHeight: 25 },
});
