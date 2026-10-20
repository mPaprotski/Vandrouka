import { createContext, useContext, useEffect, useState, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { AppState } from 'react-native';
import { api } from '../api/client';
import { getApiBaseUrl } from '../config/api';
import { createSessionController } from './session';
import type { SessionController } from './session';
import { secureSessionStorage } from './storage';

const Context=createContext<SessionController|null>(null);
export function SessionProvider({children}:{children:ReactNode}){
 const [controller]=useState(()=>createSessionController({api,storage:secureSessionStorage,origin:getApiBaseUrl()}));
 useEffect(()=>{
  void controller.resume();
  const listener=AppState.addEventListener('change',state=>{if(state==='active')void controller.resume();});
  return()=>listener.remove();
 },[controller]);
 return <Context.Provider value={controller}>{children}</Context.Provider>;
}
export function useSession(){
 const controller=useContext(Context);
 if(!controller)throw new Error('SessionProvider missing');
 const state=useSyncExternalStore(controller.subscribe,controller.getSnapshot,controller.getSnapshot);
 return {state,controller};
}
