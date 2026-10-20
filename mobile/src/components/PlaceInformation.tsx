import { useState } from 'react';
import { Linking, Platform, StyleSheet, Text, View } from 'react-native';
import type { City, PlaceDetails } from '../api/client';
import { navigationUrls, safeSourceUrl, ticketLines, weeklyHours } from '../places/presentation';
import { PlaceCover } from './PlaceCover';
import { AppButton } from './ui/AppButton';
import { colors } from './ui/theme';

export function PlaceInformation({ place, city }: { place: PlaceDetails; city?: City }) {
  const [opening, setOpening] = useState(false);
  const [linkError, setLinkError] = useState<string>();
  async function navigate() {
    if (opening) return;
    setOpening(true); setLinkError(undefined);
    try {
      for (const url of navigationUrls(place, Platform.OS)) {
        try { await Linking.openURL(url); return; } catch { /* Try browser fallback. */ }
      }
      setLinkError('Не удалось открыть навигацию. Попробуйте позже или выберите другое приложение карт.');
    } finally { setOpening(false); }
  }
  const sources = place.links.filter(link => safeSourceUrl(link.url));

  return <>
    <PlaceCover key={place.content?.cover.url ?? 'missing'} cover={place.content?.cover} title={place.name} interactive />
    {place.content && <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>История места</Text>
      <Text style={styles.body}>{place.content.story}</Text>
      <Text accessibilityRole="header" style={styles.heading}>Как подойти</Text>
      <Text style={styles.body}>{place.content.approach}</Text>
      <Text style={styles.note}>Источники просмотрены: {place.content.reviewed_on}. {place.content.field_verified ? 'Проверено на месте.' : 'Проверка подхода на месте ещё не выполнена.'}</Text>
      {place.content.sources.map(url => <AppButton key={url} title={new URL(url).hostname} variant="secondary" onPress={() => { void Linking.openURL(url).catch(() => setLinkError('Не удалось открыть источник.')); }} />)}
    </View>}
    <AppButton title="Как добраться" loading={opening} onPress={() => { void navigate(); }} />
    {linkError && <Text accessibilityRole="alert" style={styles.error}>{linkError}</Text>}
    <View style={styles.panel}>
      <Text accessibilityRole="header" style={styles.heading}>Перед прогулкой</Text>
      <Text style={styles.body}>Время осмотра: {place.visit_minutes > 0 ? `${place.visit_minutes} мин` : 'не указано'}.</Text>
      <Text style={styles.body}>{city?.name ?? place.city} · {place.lat.toFixed(5)}, {place.lon.toFixed(5)}</Text>
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
      {sources.map((link, index) => <AppButton key={`${link.url}-${index}`} title={link.title || (link.kind === 'official' ? 'Официальный сайт' : 'Источник информации')} variant="secondary" onPress={() => {
        setLinkError(undefined);
        const url = safeSourceUrl(link.url);
        if (url) void Linking.openURL(url).catch(() => setLinkError('Не удалось открыть ссылку. Попробуйте позже.'));
      }} />)}
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
