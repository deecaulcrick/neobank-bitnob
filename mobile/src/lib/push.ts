import Constants from 'expo-constants';
import * as Device from 'expo-device';
import { Platform } from 'react-native';

import { api } from './api';

// Registers this phone for push notifications, if it can be. It needs a real
// device, the user's permission and an EAS project id; in Expo Go or a
// simulator it quietly does nothing.
export async function registerForPush() {
  try {
    if (!Device.isDevice) return;
    const projectId = Constants.expoConfig?.extra?.eas?.projectId ?? Constants.easConfig?.projectId;
    if (!projectId) return;
    // Loaded lazily: the module refuses to load for remote push in Expo Go on Android.
    const Notifications = await import('expo-notifications');
    let { status } = await Notifications.getPermissionsAsync();
    if (status !== 'granted') ({ status } = await Notifications.requestPermissionsAsync());
    if (status !== 'granted') return;
    const token = await Notifications.getExpoPushTokenAsync({ projectId });
    await api.registerDevice(token.data, Platform.OS);
  } catch {
    // Push is a nicety; never let it break the app.
  }
}
