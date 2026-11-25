import {clearQuestDrafts} from '../quests/draftStorage';
import {clearWalks} from '../walks/storage';
import {clearAccountAttempts} from '../visits/attemptStorage';
import {getApiBaseUrl} from '../config/api';
import {useRef,useState} from 'react';
import {Text,View} from 'react-native';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {AppButton} from './ui/AppButton';
import {PasswordInput} from './ui/PasswordInput';
import {colors} from './ui/theme';
export function AccountDeletion({owner,session}:{owner:number;session:SessionController}){
 const [open,setOpen]=useState(false),[password,setPassword]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState<string>();const pending=useRef(false);
 async function remove(){
  if(pending.current)return;pending.current=true;setBusy(true);setError(undefined);
  try{
   // Include pre-index records for places still present in the public catalog.
   const places=await api.listPlaces('minsk');
   await session.withAccess(token=>api.deleteAccount(password,token),owner);setPassword('');
   await cleanupDeletedAccount(owner,session,places.map(place=>place.id));
  }
  catch(e){setError(e instanceof Error?e.message:'Не удалось удалить аккаунт. Проверьте результат перед повтором.');}
  finally{pending.current=false;setBusy(false);}
 }
 return <View style={{gap:12}}>{!open?<AppButton title="Удалить аккаунт" variant="secondary" onPress={()=>setOpen(true)}/>:<>
  <Text style={{color:colors.error}}>Будут удалены аккаунт, все посещения, фото и XP. Все сессии будут завершены. Отменить удаление нельзя.</Text>
  <Text style={{color:colors.secondary}}>Введите текущий пароль для окончательного удаления.</Text>
  <PasswordInput label="Текущий пароль для удаления аккаунта" value={password} onChangeText={setPassword} editable={!busy}/>
  {error?<Text accessibilityRole="alert" style={{color:colors.error}}>{error}</Text>:null}
  <AppButton title="Удалить аккаунт навсегда" loading={busy} disabled={!password} onPress={()=>void remove()}/>
  <AppButton title="Отмена" variant="secondary" disabled={busy} onPress={()=>{setOpen(false);setPassword('');setError(undefined);}}/>
 </>}</View>;
}

export async function cleanupDeletedAccount(owner:number,session:SessionController,placeIds:number[]=[]){
   let notice='Аккаунт удалён.';
   try{
    const origin=getApiBaseUrl();
    const cleanups=await Promise.allSettled([clearAccountAttempts(origin,owner,placeIds),clearQuestDrafts(origin,owner),clearWalks(origin,owner)]);
    if(cleanups.some(result=>result.status==='rejected'))throw new Error('Local cleanup incomplete');
   }
   catch{notice='Аккаунт удалён. Не удалось полностью очистить защищённые локальные записи; локальная очистка требует повторной проверки.';}
   await session.logout(notice,owner);
}
