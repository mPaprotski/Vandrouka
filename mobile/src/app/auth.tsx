import { router } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { KeyboardAvoidingView, Platform, StyleSheet, Text, TextInput, View } from 'react-native';
import { ApiError } from '../api/client';
import { useSession } from '../auth/SessionProvider';
import { SectionScreen } from '../components/SectionScreen';
import { AppButton } from '../components/ui/AppButton';
import { colors } from '../components/ui/theme';

export default function AuthScreen(){
 const {state,controller}=useSession();
 const [mode,setMode]=useState<'login'|'register'>('login');
 const [email,setEmail]=useState(''),[password,setPassword]=useState(''),[confirmation,setConfirmation]=useState('');
 const [error,setError]=useState<string>(),[requestId,setRequestId]=useState<string>();
 const [busy,setBusy]=useState(false);const submitting=useRef(false);
 useEffect(()=>{if(state.status==='authenticated')router.replace('/profile');},[state.status]);
 async function submit(){
  if(submitting.current)return;
  setError(undefined);setRequestId(undefined);
  if(!email.trim()||[...password].length<12||[...password].length>128){setError('Введите email и пароль длиной 12–128 символов.');return;}
  if(mode==='register'&&password!==confirmation){setError('Пароли не совпадают.');return;}
  submitting.current=true;setBusy(true);
  try{await controller.signIn(mode,{email,password});setPassword('');setConfirmation('');}
  catch(cause){setError(cause instanceof Error?cause.message:'Не удалось войти. Повторите попытку.');setRequestId(cause instanceof ApiError?cause.requestId:undefined);}
  finally{submitting.current=false;setBusy(false);}
 }
 return <KeyboardAvoidingView style={styles.screen} behavior={Platform.OS==='ios'?'padding':undefined}>
  <SectionScreen title={mode==='login'?'С возвращением':'Начните свою историю'} description="Войдите, чтобы сохранять свои открытия. Карту и места можно смотреть без аккаунта.">
   <View style={styles.form}>
    <Text style={styles.label}>Электронная почта</Text>
    <TextInput accessibilityLabel="Электронная почта" value={email} onChangeText={setEmail} editable={!busy} keyboardType="email-address" autoCapitalize="none" autoCorrect={false} autoComplete="email" textContentType="emailAddress" maxLength={254} placeholder="name@example.com" placeholderTextColor={colors.secondary} style={styles.input}/>
    <Text style={styles.label}>Пароль</Text>
    <TextInput accessibilityLabel="Пароль" value={password} onChangeText={setPassword} editable={!busy} secureTextEntry autoCapitalize="none" autoCorrect={false} autoComplete={mode==='register'?'new-password':'current-password'} textContentType={mode==='register'?'newPassword':'password'} style={styles.input}/>
    <Text style={styles.note}>От 12 до 128 символов. Пробелы в пароле учитываются.</Text>
    {mode==='register'&&<>
     <Text style={styles.label}>Повторите пароль</Text>
     <TextInput accessibilityLabel="Повторите пароль" value={confirmation} onChangeText={setConfirmation} editable={!busy} secureTextEntry autoCapitalize="none" autoCorrect={false} autoComplete="new-password" textContentType="newPassword" style={styles.input}/>
    </>}
    {error&&<Text accessibilityRole="alert" style={styles.error}>{error}</Text>}
    {requestId&&<Text selectable style={styles.note}>Код обращения: {requestId}</Text>}
    <AppButton title={mode==='login'?'Войти':'Создать аккаунт'} loading={busy} onPress={()=>{void submit();}}/>
    <AppButton title={mode==='login'?'У меня ещё нет аккаунта':'У меня уже есть аккаунт'} variant="secondary" disabled={busy} onPress={()=>{setMode(mode==='login'?'register':'login');setPassword('');setConfirmation('');setError(undefined);setRequestId(undefined);}}/>
    <AppButton title="Продолжить без входа" variant="secondary" disabled={busy} onPress={()=>router.replace('/explore')}/>
   </View>
  </SectionScreen>
 </KeyboardAvoidingView>;
}
const styles=StyleSheet.create({
 screen:{flex:1,backgroundColor:colors.background},form:{gap:12},
 label:{fontSize:15,color:colors.primary,fontWeight:'600'},
 input:{backgroundColor:colors.surface,borderColor:colors.border,borderWidth:1,borderRadius:14,padding:16,minHeight:56,fontSize:16,color:colors.primary},
 note:{color:colors.secondary,fontSize:13,lineHeight:20},error:{color:colors.error,fontSize:15,lineHeight:22},
});
