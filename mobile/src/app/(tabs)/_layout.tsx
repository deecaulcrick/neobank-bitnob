import { Tabs } from 'expo-router';

import { TabBar } from '../../components/TabBar';

// Three tabs: balance, the keypad, history. Profile sits behind the avatar.
export default function TabsLayout() {
  return (
    <Tabs tabBar={(props) => <TabBar {...props} />} screenOptions={{ headerShown: false }}>
      <Tabs.Screen name="index" options={{ title: 'Home' }} />
      <Tabs.Screen name="pay" options={{ title: 'Pay' }} />
      <Tabs.Screen name="activity" options={{ title: 'Activity' }} />
    </Tabs>
  );
}
