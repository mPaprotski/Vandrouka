import {useCallback,useMemo,useSyncExternalStore} from 'react';
import {AppState} from 'react-native';
import {useFocusEffect} from 'expo-router';
import {createPrivateResource} from '../sync/privateResource';
/** load must be stable and pin the authenticated owner. No private stale data. */
export function usePrivateResource<T>(load:(signal:AbortSignal)=>Promise<T>){
 const resource=useMemo(()=>createPrivateResource(load),[load]);
 const state=useSyncExternalStore(resource.subscribe,resource.getSnapshot,resource.getSnapshot);
 useFocusEffect(useCallback(()=>{
  resource.setActive(AppState.currentState==='active');resource.setFocused(true);
  const listener=AppState.addEventListener('change',value=>resource.setActive(value==='active'));
  return()=>{listener.remove();resource.setFocused(false);resource.setActive(false);};
 },[resource]));
 const retry=useCallback(()=>{void resource.refresh();},[resource]);
 return {state,retry};
}
