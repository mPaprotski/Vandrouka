import {useState} from 'react';
import {Text,View} from 'react-native';
import {useSession} from '../auth/SessionProvider';
import {beginOAuth,cancelOAuth} from '../auth/oauth-flow';
import {AppButton} from './ui/AppButton';

export function SocialLoginButtons({target='/profile',disabled=false}:{target?:string;disabled?:boolean}){
 const {controller}=useSession();
 const [busy,setBusy]=useState(false),[pending,setPending]=useState(false),[error,setError]=useState('');
 async function start(provider:'google'|'yandex'){
  if(busy)return;
  setBusy(true);setError('');
  try{await beginOAuth(provider,'login',controller,undefined,'',target);setPending(true);}
  catch(e){setError(e instanceof Error?e.message:'Не удалось начать вход');}
  finally{setBusy(false);}
 }
 return <View style={{gap:12}}>
  <AppButton title="Продолжить с Google" disabled={disabled||busy} onPress={()=>void start('google')}/>
  <AppButton title="Продолжить с Яндексом" disabled={disabled||busy} onPress={()=>void start('yandex')}/>
  {pending&&<><Text>Завершите вход в браузере и нажмите «Вернуться в Вандроўку».</Text>
   <AppButton title="Отменить вход через браузер" variant="secondary" onPress={()=>void cancelOAuth().then(()=>setPending(false))}/></>}
  {!!error&&<Text accessibilityRole="alert">{error}</Text>}
 </View>;
}
