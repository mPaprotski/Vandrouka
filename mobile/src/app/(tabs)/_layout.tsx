import { Tabs } from 'expo-router';
import { Text } from 'react-native';

export default function MainTabs() {
  return (
    <Tabs initialRouteName="explore" backBehavior="initialRoute" screenOptions={{
      headerStyle: { backgroundColor: '#F7F4EC' },
      headerTintColor: '#173F34',
      tabBarActiveTintColor: '#173F34',
      tabBarInactiveTintColor: '#627066',
      tabBarStyle: { backgroundColor: '#F7F4EC', borderTopColor: '#D5DCCF' },
      tabBarLabelStyle: { fontSize: 12 },
    }}>
      <Tabs.Screen name="explore" options={{ title: 'Исследовать', tabBarIcon: ({ color }) => <Text style={{ color, fontSize: 22 }}>⌖</Text> }} />
      <Tabs.Screen name="visits" options={{ title: 'Посещения', tabBarIcon: ({ color }) => <Text style={{ color, fontSize: 22 }}>◷</Text> }} />
      <Tabs.Screen name="profile" options={{ title: 'Профиль', tabBarIcon: ({ color }) => <Text style={{ color, fontSize: 22 }}>○</Text> }} />
    </Tabs>
  );
}
