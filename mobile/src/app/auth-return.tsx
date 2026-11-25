import {router,useLocalSearchParams} from 'expo-router';
import {useEffect,useRef,useState} from 'react';
import {Text} from 'react-native';
import {useSession} from '../auth/SessionProvider';
import {finishOAuth,readOAuthAttempt} from '../auth/oauth-flow';
import {SectionScreen} from '../components/SectionScreen';
import {AppButton} from '../components/ui/AppButton';
import {cleanupDeletedAccount} from '../components/AccountDeletion';

export default function AuthReturn(){
 const {attempt,code}=useLocalSearchParams<{attempt?:string;code?:string}>();
 const {controller}=useSession();
 const [purpose,setPurpose]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(false);
 const started=useRef(false);
 async function finish(){
  if(typeof attempt!=='string'||typeof code!=='string'){setError('Некорректная ссылка возврата');return;}
  setBusy(true);setError('');
  try{
   await controller.resume();
   const a=await finishOAuth(attempt,code,controller);
   if(a.purpose==='delete'&&a.owner){await cleanupDeletedAccount(a.owner,controller);router.replace('/profile');}
   else if(a.purpose==='login'&&a.target.startsWith('/capture/'))router.replace({pathname:'/capture/[id]',params:{id:a.target.split('/').pop()!}});
   else router.replace(a.purpose==='login'&&a.target==='/quests'?'/quests':'/profile');
  }catch(e){setError(e instanceof Error?e.message:'Не удалось завершить вход');}
  finally{setBusy(false);}
 }
 useEffect(()=>{
  if(started.current)return;started.current=true;
  void readOAuthAttempt().then(a=>{
   if(a.id!==attempt)throw new Error('Попытка входа устарела');
   setPurpose(a.purpose);
   if(a.purpose!=='delete')void finish();
  }).catch(e=>setError(e.message));
 // The screen handles the original handoff once.
 // eslint-disable-next-line react-hooks/exhaustive-deps
 },[]);
 return <SectionScreen title={purpose==='delete'?'Удаление аккаунта':'Вход в Вандроўку'}>
  {purpose==='delete'&&<><Text>Будут удалены профиль, фотографии, посещения и XP. Отменить удаление нельзя.</Text>
   <AppButton title="Удалить аккаунт навсегда" disabled={busy} onPress={()=>void finish()}/></>}
  {busy&&<Text>Завершаем действие…</Text>}
  {!!error&&<Text accessibilityRole="alert">{error}</Text>}
  <AppButton title="Вернуться в профиль" variant="secondary" disabled={busy} onPress={()=>router.replace('/profile')}/>
 </SectionScreen>;
}
