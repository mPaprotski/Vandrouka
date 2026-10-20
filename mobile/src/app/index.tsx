import { AppButton } from '../components/ui/AppButton';
import { Link } from 'expo-router';
import { useState } from 'react';
import { Platform, ScrollView, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

const steps = [
  ['01', 'Заметьте больше', 'Находите исторические места и узнавайте истории города.'],
  ['02', 'Сделайте открытие', 'Дойдите до места и сохраните фотографию своей прогулки.'],
  ['03', 'Соберите свою историю', 'Открытия останутся на личной карте путешествий.'],
];

export default function WelcomeScreen() {
  const [showSteps, setShowSteps] = useState(false);

  return (
    <SafeAreaView style={styles.screen}>
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.header}>
          <Text style={styles.brand}>вандроўка</Text>
          <Text style={styles.city}>МИНСК</Text>
        </View>

        <View style={styles.illustration} accessible={false}>
          <View style={styles.orbit} />
          <View style={styles.orbitSmall} />
          <View style={styles.marker}><Text style={styles.markerText}>В</Text></View>
          <View style={styles.smallMarker} />
          <Text style={styles.illustrationCaption}>ЗНАКОМЫЙ ГОРОД. НОВЫЕ ОТКРЫТИЯ.</Text>
        </View>

        <Text style={styles.eyebrow}>ИСТОРИИ БЛИЖЕ, ЧЕМ КАЖЕТСЯ</Text>
        <Text style={styles.title}>Город хранит истории.{ '\n' }Найдите свою.</Text>
        <Text style={styles.description}>
          Гуляйте по Минску, открывайте культурные места и сохраняйте воспоминания.
          Ваша следующая история начинается с прогулки.
        </Text>

        <Link href="/explore" replace style={styles.startLink}>Начать прогулку →</Link>

        <AppButton
          title={showSteps ? 'Скрыть подсказки' : 'Как исследовать город'}
          expanded={showSteps}
          onPress={() => setShowSteps(value => !value)}
        />

        {showSteps && (
          <View style={styles.steps}>
            {steps.map(([number, title, description]) => (
              <View key={number} style={styles.step}>
                <Text style={styles.stepNumber}>{number}</Text>
                <View style={styles.stepCopy}>
                  <Text style={styles.stepTitle}>{title}</Text>
                  <Text style={styles.stepDescription}>{description}</Text>
                </View>
              </View>
            ))}
          </View>
        )}
        <Text style={styles.footer}>Каждое место — часть вашей истории.</Text>
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#F7F4EC' },
  content: { flexGrow: 1, paddingHorizontal: 26, paddingTop: 16, paddingBottom: 24, maxWidth: 560, width: '100%', alignSelf: 'center' },
  header: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  brand: { color: '#173F34', fontSize: 27, fontWeight: '700', letterSpacing: -1 },
  city: { color: '#456457', fontSize: 10, letterSpacing: 2, borderWidth: 1, borderColor: '#CED8CB', borderRadius: 18, paddingHorizontal: 13, paddingVertical: 8 },
  illustration: { height: 240, alignItems: 'center', justifyContent: 'center', marginTop: 12, marginBottom: 20 },
  orbit: { width: 184, height: 184, borderRadius: 92, borderWidth: 1, borderColor: '#D5DCCF', transform: [{ scaleX: 1.25 }, { rotate: '-24deg' }] },
  orbitSmall: { position: 'absolute', width: 126, height: 126, borderRadius: 63, borderWidth: 1, borderStyle: 'dashed', borderColor: '#A5B8A7' },
  marker: { position: 'absolute', width: 66, height: 78, backgroundColor: '#173F34', borderRadius: 30, borderBottomRightRadius: 4, alignItems: 'center', justifyContent: 'center', transform: [{ rotate: '-8deg' }] },
  markerText: { color: '#F7F4EC', fontSize: 36, fontFamily: Platform.OS === 'ios' ? 'Georgia' : 'serif' },
  smallMarker: { position: 'absolute', width: 18, height: 18, borderRadius: 9, backgroundColor: '#CC8245', top: 49, right: 59, borderWidth: 4, borderColor: '#F7F4EC' },
  illustrationCaption: { position: 'absolute', bottom: 0, color: '#66786B', fontSize: 9, letterSpacing: 1.7 },
  eyebrow: { color: '#8A633F', fontSize: 10, letterSpacing: 1.6, marginBottom: 14 },
  title: { color: '#173F34', fontSize: 39, lineHeight: 45, letterSpacing: -1.3, fontFamily: Platform.OS === 'ios' ? 'Georgia' : 'serif' },
  description: { color: '#627066', fontSize: 16, lineHeight: 25, marginTop: 18, marginBottom: 28 },
  startLink: { color: '#173F34', fontSize: 18, fontWeight: '700', paddingVertical: 18, marginBottom: 12 },
  steps: { gap: 20, marginTop: 26 },
  step: { flexDirection: 'row', gap: 14 },
  stepNumber: { color: '#9A744F', fontSize: 12, paddingTop: 3 },
  stepCopy: { flex: 1 },
  stepTitle: { color: '#173F34', fontWeight: '600', fontSize: 15, marginBottom: 4 },
  stepDescription: { color: '#627066', fontSize: 14, lineHeight: 21 },
  footer: { color: '#7C867B', textAlign: 'center', fontSize: 11, marginTop: 26 },
});
