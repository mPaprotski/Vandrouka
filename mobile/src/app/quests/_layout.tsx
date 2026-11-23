import { router, Stack } from 'expo-router';
import { Pressable, Text } from 'react-native';
import { colors } from '../../components/ui/theme';

function Back() {
  return <Pressable accessibilityRole="button" accessibilityLabel="Назад" hitSlop={12}
    onPress={() => { if (router.canGoBack()) router.back(); else router.replace('/quests'); }}
    style={{ minHeight: 44, justifyContent: 'center', paddingRight: 14 }}>
    <Text style={{ color: colors.primary, fontSize: 16 }}>‹ Назад</Text>
  </Pressable>;
}
export default function QuestLayout() {
  return <Stack screenOptions={{
    headerStyle: { backgroundColor: colors.background }, headerTintColor: colors.primary,
    contentStyle: { backgroundColor: colors.background }, headerLeft: () => <Back />,
  }}>
    <Stack.Screen name="join" options={{ title: 'Присоединиться' }} />
    <Stack.Screen name="new" options={{ title: 'Новый квест' }} />
    <Stack.Screen name="edit/[questId]" options={{ title: 'Маршрут квеста' }} />
    <Stack.Screen name="run/[runId]" options={{ title: 'Управление прогулкой' }} />
    <Stack.Screen name="play/[participationId]/index" options={{ title: 'Фотоквест' }} />
    <Stack.Screen name="play/[participationId]/capture" options={{ title: 'Фото задания' }} />
  </Stack>;
}
