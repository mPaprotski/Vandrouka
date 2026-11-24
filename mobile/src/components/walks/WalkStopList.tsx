import { router } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';
import type { WalkStop } from '../../walks/types';
import { distance, duration, walkTime } from '../../walks/format';
import { AppButton } from '../ui/AppButton';
import { colors } from '../ui/theme';

export function WalkStopList({ stops, timezone, selectedId, onSelect, onRemove, disabled = false, visits, onConfirm }: {
  stops: WalkStop[]; timezone: string; selectedId?: number; onSelect?: (id: number) => void; onRemove?: (id: number) => void; disabled?: boolean;
  visits?: Record<string,number>; onConfirm?: (id:number) => void;
}) {
  return <View style={{ gap: 16 }}>{stops.map((stop, index) => <View key={stop.place_id} style={[styles.stop, selectedId === stop.place_id && { borderColor: colors.primary, borderWidth: 2 }]}>
    <Text accessibilityRole="header" style={styles.title}>{index + 1}. {stop.name}</Text>
    {visits?.[String(stop.place_id)] !== undefined && <Text>Посещение подтверждено ✓</Text>}
    {onConfirm && visits?.[String(stop.place_id)] === undefined && <AppButton title="Подтвердить посещение" onPress={()=>onConfirm(stop.place_id)} />}
    <Text style={styles.text}>{index === 0 ? 'От старта' : 'От предыдущего места'} — {duration(stop.incoming_leg.walk_seconds)}, {distance(stop.incoming_leg.distance_m)}</Text>
    <Text style={styles.text}>Прибытие — {walkTime(stop.arrival, timezone)}</Text>
    <Text style={styles.text}>Отправление — {walkTime(stop.departure, timezone)} · Осмотр — {duration(stop.visit_seconds)}</Text>
    {stop.hours_unconfirmed && <Text style={styles.warning}>Часы работы не подтверждены</Text>}
    {stop.price_unconfirmed && <Text style={styles.warning}>Стоимость посещения не подтверждена</Text>}
    <AppButton title="Открыть место" accessibilityLabel={`Открыть место: ${stop.name}`} variant="secondary" onPress={() => router.push({ pathname: '/places/[id]', params: { id: stop.place_id } })} />
    {onSelect && <AppButton title="Выделить на карте" variant="secondary" onPress={() => onSelect(stop.place_id)} />}
    {onRemove && <AppButton title="Убрать остановку" variant="secondary" disabled={disabled} onPress={() => onRemove(stop.place_id)} />}
  </View>)}</View>;
}
const styles = StyleSheet.create({
  stop: { padding: 18, gap: 10, backgroundColor: colors.surface, borderRadius: 16, borderWidth: 1, borderColor: colors.border },
  title: { color: colors.primary, fontSize: 20, fontWeight: '600' },
  text: { color: colors.secondary, fontSize: 16, lineHeight: 24 },
  warning: { color: colors.accent, fontSize: 15 },
});
