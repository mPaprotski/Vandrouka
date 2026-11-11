import {useRef,useState} from 'react';
import {Text,View} from 'react-native';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {AppButton} from './ui/AppButton';
import {colors} from './ui/theme';
export function VisitDeletion({id,photo,owner,session,onRemoved}:{id:number;photo:string|null;owner:number;session:SessionController;onRemoved:()=>void}){
 const [choice,setChoice]=useState<'photo'|'visit'|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState<string>();const pending=useRef(false);
 async function remove(){if(!choice||pending.current)return;pending.current=true;setBusy(true);setError(undefined);
  try{await session.withAccess(token=>choice==='photo'&&photo?api.deletePhoto(photo,token):api.hideVisit(id,token),owner);onRemoved();setChoice(null);}
  catch(e){setError(e instanceof Error?e.message:'Не удалось удалить. Повторите запрос.');}finally{pending.current=false;setBusy(false);}}
 return <View style={{gap:10}}>{choice?<>
  <Text style={{color:colors.secondary}}>{choice==='photo'?'Удалить фото? Посещение и XP сохранятся.':'Удалить посещение и фото из истории? XP сохранится; повторной награды за место не будет.'}</Text>
  <AppButton title="Подтвердить удаление" loading={busy} onPress={()=>void remove()}/>
  <AppButton title="Отмена" variant="secondary" disabled={busy} onPress={()=>setChoice(null)}/>
 </>:<>{photo?<AppButton title="Удалить фото" variant="secondary" onPress={()=>setChoice('photo')}/>:null}<AppButton title="Удалить из истории" variant="secondary" onPress={()=>setChoice('visit')}/></>}
 {error?<Text accessibilityRole="alert" style={{color:colors.error}}>{error}</Text>:null}</View>;
}
