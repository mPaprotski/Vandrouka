import { Pressable, StyleSheet, Text, View } from 'react-native';
import type { CatalogCover } from '../../api/client';
import { PlaceCover } from '../PlaceCover';
import { colors } from './theme';

export function PlaceCard({ title, categories = [], description, cover, onPress }: {
  title: string; categories?: readonly string[]; description?: string; cover?: CatalogCover; onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`Открыть место: ${title}`}
      onPress={onPress}
      style={({ pressed }) => [styles.card, pressed && styles.pressed]}
    >
      {cover && <PlaceCover key={cover.url} cover={cover} title={title} />}
      <Text style={styles.title}>{title}</Text>
      {categories.length > 0 && (
        <View style={styles.categories}>
          {Array.from(new Set(categories)).map(category => <Text key={category} style={styles.category}>{category}</Text>)}
        </View>
      )}
      {description ? <Text numberOfLines={3} style={styles.description}>{description}</Text> : null}
      <Text style={styles.open}>Подробнее →</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  card: { minHeight: 56, borderRadius: 20, backgroundColor: colors.surface, borderColor: colors.border, borderWidth: 1, padding: 22, gap: 12 },
  title: { color: colors.primary, fontSize: 20, fontWeight: '600' },
  categories: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  category: { color: colors.accent, fontSize: 12, paddingHorizontal: 10, paddingVertical: 6, borderRadius: 12, backgroundColor: colors.background },
  description: { color: colors.secondary, fontSize: 15, lineHeight: 23 },
  open: { color: colors.primary, fontSize: 15, fontWeight: '600' },
  pressed: { opacity: 0.75 },
});
