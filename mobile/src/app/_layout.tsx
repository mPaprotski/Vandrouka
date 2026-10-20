import { SessionProvider } from '../auth/SessionProvider';
import { router, Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { Pressable, Text } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

function BackButton() {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Назад к местам"
      hitSlop={12}
      onPress={() => router.canGoBack() ? router.back() : router.replace('/explore')}
      style={{ minHeight: 44, justifyContent: 'center', paddingRight: 16 }}
    >
      <Text style={{ color: '#173F34', fontSize: 16 }}>‹ Назад</Text>
    </Pressable>
  );
}

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <SessionProvider>
 <StatusBar style="dark" />
      <Stack screenOptions={{
        headerStyle: { backgroundColor: '#F7F4EC' },
        headerTintColor: '#173F34',
        contentStyle: { backgroundColor: '#F7F4EC' },
      }}>
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="places/[id]" options={{ title: 'Место', headerLeft: () => <BackButton /> }} />
        <Stack.Screen name="capture/[id]" options={{title:"Фото места",headerLeft:()=> <BackButton />}} />
        <Stack.Screen name="auth" options={{title:'Вход в Вандроўку',headerLeft:()=> <BackButton />}} />
      </Stack>
      </SessionProvider>
    </SafeAreaProvider>
  );
}
