import { router, Stack } from 'expo-router';
import { Pressable, Text } from 'react-native';
import { WalkProvider } from '../../walks/WalkProvider';
export default function WalkLayout() {
  return <WalkProvider><Stack>
    <Stack.Screen name="index" options={{ title: 'Параметры прогулки', headerLeft: () => <Pressable accessibilityRole="button" accessibilityLabel="Назад к местам" style={{ minHeight: 44, justifyContent: 'center', paddingRight: 16 }} onPress={() => router.canGoBack() ? router.back() : router.replace('/explore')}><Text>‹ Назад</Text></Pressable> }} />
    <Stack.Screen name="variants" options={{ title: 'Варианты прогулки' }} />
    <Stack.Screen name="preview" options={{ title: 'Маршрут прогулки' }} />
    <Stack.Screen name="active" options={{ title: 'Моя прогулка' }} />
  </Stack></WalkProvider>;
}
