import { StyleSheet, Text, View } from 'react-native';
import { AppButton } from '../ui/AppButton';
import { colors } from '../ui/theme';
import { distance, duration, ticketCost } from '../../walks/format';
import type { WalkVariant } from '../../walks/types';

export function WalkVariantCard({ variant, title, onSelect }: { variant: WalkVariant; title: string; onSelect?: () => void }) {
  return <View style={styles.card}>
    <Text accessibilityRole="header" style={styles.title}>{title}</Text>
    {variant.labels.length > 0 && <Text style={styles.text}>{variant.labels.join(' · ')}</Text>}
    <Text style={styles.text}>{duration(variant.totals.total_seconds)} · {distance(variant.totals.distance_m)} · Остановок — {variant.stops.length}</Text>
    <Text style={styles.text}>В пути — {duration(variant.totals.walk_seconds)} · Осмотр — {duration(variant.totals.visit_seconds)}</Text>
    <Text style={styles.text}>{ticketCost(variant.totals)}</Text>
    <Text style={styles.text}>{variant.stops.slice(0, 3).map(s => s.name).join(' → ')}{variant.stops.length > 3 ? ' …' : ''}</Text>
    {variant.explanation.text ? <Text style={styles.text}>{variant.explanation.text}</Text> : null}
    {onSelect && <AppButton title="Посмотреть маршрут" accessibilityLabel={`Посмотреть ${title.toLowerCase()}`} onPress={onSelect} />}
  </View>;
}
const styles = StyleSheet.create({
  card: { padding: 18, gap: 12, borderWidth: 1, borderColor: colors.border, borderRadius: 16, backgroundColor: colors.surface },
  title: { fontSize: 20, fontWeight: '600', color: colors.primary },
  text: { color: colors.secondary, fontSize: 16, lineHeight: 24 },
});
