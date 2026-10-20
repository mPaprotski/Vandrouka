import { useState } from 'react';
import { Image, Linking, StyleSheet, Text, View } from 'react-native';
import type { CatalogCover } from '../api/client';
import { getApiBaseUrl } from '../config/api';
import { AppButton } from './ui/AppButton';
import { colors } from './ui/theme';

export function PlaceCover({ cover, title, interactive = false }: { cover?: CatalogCover; title: string; interactive?: boolean }) {
  const [failed, setFailed] = useState(false);
  const [linkError, setLinkError] = useState(false);
  let uri: string | undefined;
  if (cover) { try { uri = new URL(cover.url, getApiBaseUrl()).href; } catch { /* Keep the rest of the card usable. */ } }
  return <View style={styles.container}>
    {uri && !failed
      ? <Image source={{ uri }} accessibilityLabel={`Фотография: ${title}`} resizeMode="contain" style={styles.image} onError={() => setFailed(true)} />
      : <View style={styles.placeholder}><Text style={styles.caption}>{cover ? 'Не удалось загрузить фотографию' : 'Фотография пока не добавлена'}</Text></View>}
    {cover && <>
      <Text style={styles.caption}>Фото: {cover.author} · {cover.license}</Text>
      {interactive && <>
        <Text style={styles.caption}>{cover.changes}</Text>
        <AppButton title="Автор и оригинал фотографии" variant="secondary" onPress={() => { void Linking.openURL(cover.source_url).catch(() => setLinkError(true)); }} />
        <AppButton title={`Лицензия ${cover.license}`} variant="secondary" onPress={() => { void Linking.openURL(cover.license_url).catch(() => setLinkError(true)); }} />
        {linkError && <Text accessibilityRole="alert" style={styles.caption}>Не удалось открыть ссылку.</Text>}
      </>}
    </>}
  </View>;
}
const styles = StyleSheet.create({
  container: { gap: 10 },
  image: { width: '100%', height: 220, borderRadius: 14, backgroundColor: '#E8E7DB' },
  placeholder: { minHeight: 100, padding: 18, borderRadius: 14, backgroundColor: '#E8E7DB', justifyContent: 'center', alignItems: 'center' },
  caption: { color: colors.secondary, fontSize: 12, lineHeight: 18 },
});
