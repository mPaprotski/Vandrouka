import {createOwnedStore} from './ownedStore';
import * as SecureStore from 'expo-secure-store';
import * as Crypto from 'expo-crypto';
import {createAttemptStorage} from './attempt';
const options={keychainAccessible:SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY};
const store={
 async get(key:string){if(!await SecureStore.isAvailableAsync())throw Error('Secure storage unavailable');return SecureStore.getItemAsync(key,options);},
 async set(key:string,value:string|null){if(!await SecureStore.isAvailableAsync())throw Error('Secure storage unavailable');if(value===null)await SecureStore.deleteItemAsync(key,options);else await SecureStore.setItemAsync(key,value,options);},
};
const prefix=async(origin:string,owner:number)=>`vandrouka.visit.v1.${await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256,origin)}.${owner}`;
/** Bound to backend/account/place; encrypted GPS exists only while outcome is unresolved. */
export function secureAttemptStorage(origin:string,owner:number,place:number){
 const ownerPrefix=prefix(origin,owner);
 return createAttemptStorage(origin,owner,place,ownerPrefix.then(p=>`${p}.${place}`),{
  async get(key){return createOwnedStore(await ownerPrefix,store).get(key);},
  async set(key,value){return createOwnedStore(await ownerPrefix,store).set(key,value);},
 });
}
export async function clearAccountAttempts(origin:string,owner:number,legacyPlaces:readonly number[]=[]){
 await createOwnedStore(await prefix(origin,owner),store).clear(legacyPlaces);
}
