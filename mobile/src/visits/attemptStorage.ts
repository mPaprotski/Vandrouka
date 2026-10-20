import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import {createAttemptStorage} from './attempt';
const options={keychainAccessible:SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY};
/** Bound to backend/account/place; encrypted GPS exists only while outcome is unresolved. */
export function secureAttemptStorage(origin:string,owner:number,place:number){
 const key=Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256,origin).then(hash=>`vandrouka.visit.v1.${hash}.${owner}.${place}`);
 return createAttemptStorage(origin,owner,place,key,{
  async get(key){
   if(!await SecureStore.isAvailableAsync())throw new Error('Secure storage unavailable');
   return SecureStore.getItemAsync(key,options);
  },
  async set(key,value){
   if(!await SecureStore.isAvailableAsync())throw new Error('Secure storage unavailable');
   if(value===null)await SecureStore.deleteItemAsync(key,options);
   else await SecureStore.setItemAsync(key,value,options);
  },
 });
}
