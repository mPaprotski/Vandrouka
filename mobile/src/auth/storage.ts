import * as SecureStore from 'expo-secure-store';
import type { SessionStorage } from './session';

const key='vandrouka.auth.v1';
const options={keychainAccessible:SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY};
export const secureSessionStorage:SessionStorage={
 async read(){
  if(!await SecureStore.isAvailableAsync())throw new Error('Secure storage unavailable');
  return SecureStore.getItemAsync(key,options);
 },
 async write(value){
  if(!await SecureStore.isAvailableAsync())throw new Error('Secure storage unavailable');
  await SecureStore.setItemAsync(key,value,options);
 },
};
