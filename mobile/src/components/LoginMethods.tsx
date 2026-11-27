import {router,useFocusEffect} from 'expo-router';
import {useCallback,useState} from 'react';
import {Text,View} from 'react-native';
import {api} from '../api/client';
import {useSession} from '../auth/SessionProvider';
import {beginOAuth} from '../auth/oauth-flow';
import {AppButton} from './ui/AppButton';
import {PasswordInput} from './ui/PasswordInput';

export function LoginMethods(){
 const {state,controller}=useSession();
 const owner=state.status==='authenticated'?state.user.id:undefined;
 const [methods,setMethods]=useState<{password:boolean;providers:('google'|'yandex')[]}>();
 const [password,setPassword]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');
 useFocusEffect(useCallback(()=>{
  let active=true;
  if(owner)void controller.withAccess(token=>api.oauth.methods(token),owner).then(m=>{if(active)setMethods(m);}).catch(e=>{if(active)setError(e.message);});
  return()=>{active=false;};
 },[controller,owner]));
 async function start(provider:'google'|'yandex',purpose:'link'|'delete'){
  if(busy||!owner)return;setBusy(true);setError('');
  try{await beginOAuth(provider,purpose,controller,owner,password);setPassword('');}
  catch(e){setError(e instanceof Error?e.message:'Не удалось подтвердить аккаунт');}
  finally{setBusy(false);}
 }
 return <View style={{gap:12}}>
  <Text accessibilityRole="header">Способы входа</Text>
  {methods&&!methods.password&&<Text>Для подключения нового способа входа войдите заново через подключённый аккаунт. Подключение доступно в течение пяти минут после входа.</Text>}
  {methods?.password&&<PasswordInput label="Текущий пароль для подключения нового способа входа" value={password} onChangeText={setPassword} editable={!busy}/>}
  {methods&&(['google','yandex'] as const).map(p=><View key={p} style={{gap:8}}>
   <Text>{p==='google'?'Google':'Яндекс'}{methods.providers.includes(p)?' подключён':''}</Text>
   {!methods.providers.includes(p)&&<AppButton title={p==='google'?'Подключить Google':'Подключить Яндекс'} disabled={busy} onPress={()=>void start(p,'link')}/>}
   {methods.providers.includes(p)&&<AppButton title={p==='google'?'Подтвердить удаление через Google':'Подтвердить удаление через Яндекс'} variant="secondary" disabled={busy} onPress={()=>void start(p,'delete')}/>}
  </View>)}
  {!!error&&<Text accessibilityRole="alert">{error}</Text>}
  <AppButton title="К профилю" variant="secondary" onPress={()=>router.replace('/profile')}/>
 </View>;
}
