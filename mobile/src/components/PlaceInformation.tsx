import { useState } from 'react';
import { Linking, Platform, StyleSheet, Text, View } from 'react-native';
import type { City, PlaceDetails } from '../api/client';
import { navigationUrls, placeSources, ticketLines, weeklyHours } from '../places/presentation';
import { PlaceCover } from './PlaceCover';
import { AppButton } from './ui/AppButton';
import { colors } from './ui/theme';

export function PlaceInformation({ place, city }: { place: PlaceDetails; city?: City }) {
  const [opening, setOpening] = useState(false);
  const [mapError, setMapError] = useState<string>();
  const [sourceError, setSourceError] = useState<string>();
  async function navigate() {
    if (opening) return;
    setOpening(true); setMapError(undefined);
    try {
      for (const url of navigationUrls(place, Platform.OS)) {
        try { await Linking.openURL(url); return; } catch { /* Try browser fallback. */ }
      }
      setMapError('Не удалось открыть место в картах. Попробуйте позже.');
    } finally { setOpening(false); }
  }
  const sources = placeSources(place);

  return <>
    <PlaceCover key={place.content?.cover.url ?? 'missing'} cover={place.content?.cover} title={place.name} interactive />
    <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Адрес</Text>
      {!place.address?.trim() && <Text style={styles.body}>Адрес пока не указан.</Text>}
      <AppButton title={place.address?.trim() || 'Открыть место на карте'} accessibilityLabel={`${place.address?.trim() || place.name}. Открыть в картах`} variant="secondary" loading={opening} onPress={() => { void navigate(); }} />
      {mapError && <Text accessibilityRole="alert" style={styles.error}>{mapError}</Text>}
    </View>
    {place.content && <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Как подойти</Text>
      <Text style={styles.body}>{place.content.approach}</Text>
    </View>}
    <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Перед прогулкой</Text>
      <Text style={styles.body}>Время осмотра — {place.visit_minutes > 0 ? `${place.visit_minutes} мин` : 'не указано'}.</Text>
    </View>
    <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Билеты</Text>
      {ticketLines(place).map((line, index) => <Text key={index} style={styles.body}>{line}</Text>)}
    </View>
    <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Часы работы</Text>
      {place.hours_known && place.opening_hours.length > 0
        ? weeklyHours(place.opening_hours).map(line => <Text key={line} style={styles.body}>{line}</Text>)
        : <Text style={styles.body}>Расписание неизвестно — уточните перед посещением.</Text>}
      <Text style={styles.note}>Время указано по часовому поясу города{city ? `: ${city.timezone}` : ''}. Перед посещением проверьте актуальность расписания.</Text>
      {place.opening_exceptions.length > 0 && <>
        <Text style={styles.heading}>Особые даты</Text>
        {place.opening_exceptions.map((exception, index) => <Text key={`${exception.date}-${index}`} style={styles.body}>
          {exception.date}: {exception.closed ? 'закрыто' : exception.open_time && exception.close_time ? `${exception.open_time}–${exception.close_time}` : 'время уточняется'}{exception.note ? ` — ${exception.note}` : ''}
        </Text>)}
      </>}
    </View>
    {sources.length > 0 && <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Источники и подробности</Text>
      {sources.map(link => <AppButton key={link.url} title={link.title} variant="secondary" onPress={() => {
        setSourceError(undefined);
        void Linking.openURL(link.url).catch(() => setSourceError('Не удалось открыть ссылку. Попробуйте позже.'));
      }} />)}
      {sourceError && <Text accessibilityRole="alert" style={styles.error}>{sourceError}</Text>}
      {place.verified_at && <Text style={styles.note}>Данные проверены: {place.verified_at.slice(0, 10)}</Text>}
    </View>}
  </>;
}

const styles = StyleSheet.create({
  panel: { padding: 20, gap: 12, borderRadius: 18, borderWidth: 1, borderColor: colors.border, backgroundColor: colors.surface },
  heading: { color: colors.primary, fontSize: 19, fontWeight: '600' },
  body: { color: colors.primary, fontSize: 16, lineHeight: 25 },
  note: { color: colors.secondary, fontSize: 13, lineHeight: 21 },
  error: { color: colors.error, fontSize: 15, lineHeight: 23 },
});
