import { router } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useSession } from '../../auth/SessionProvider';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState, LoadingState } from '../../components/ui/ContentState';
import { colors } from '../../components/ui/theme';

export default function ProfileScreen(){
 const {state,controller}=useSession();const [exiting,setExiting]=useState(false);const [error,setError]=useState<string>();
 async function logout(){if(exiting)return;setExiting(true);setError(undefined);try{await controller.logout();}catch(cause){setError(cause instanceof Error?cause.message:'Не удалось завершить выход. Повторите попытку.');}finally{setExiting(false);}}
 return <SectionScreen title="Ваша история города" description="Сохраняйте открытия и следите за своим прогрессом.">
  {state.status==='loading'&&<LoadingState/>}
  {state.status==='authenticated'&&<View style={styles.panel}>
   <Text accessibilityRole="header" style={styles.title}>Вы вошли в Вандроўку</Text>
   <Text selectable style={styles.text}>{state.user.email}</Text>
   <Text style={styles.text}>Опыт и уровни появятся после подключения подтверждённых посещений.</Text>
   <AppButton title="Исследовать город" onPress={()=>router.navigate('/explore')}/>
   <AppButton title="Выйти из аккаунта" variant="secondary" loading={exiting} onPress={()=>{void logout();}}/>
  </View>}
  {state.status==='guest'&&<>
   {state.message&&<Text accessibilityRole="alert" style={styles.text}>{state.message}</Text>}
   <EmptyState title="Войдите, чтобы сохранять открытия" description="Аккаунт свяжет ваши посещения и фотографии между запусками приложения." action={<AppButton title="Войти или зарегистрироваться" onPress={()=>router.push('/auth')}/>}/>
   {state.message&&<AppButton title="Повторить подключение" variant="secondary" onPress={()=>{void controller.resume();}}/>}
  </>}
  {state.status==='error'&&<View style={styles.panel}>
   <Text accessibilityRole="alert" style={styles.text}>{state.message}</Text>
   {state.requestId&&<Text selectable style={styles.text}>Код обращения: {state.requestId}</Text>}
   <AppButton title="Повторить проверку сессии" onPress={()=>{void controller.resume();}}/>
   <AppButton title="Завершить вход на этом устройстве" variant="secondary" loading={exiting} onPress={()=>{void logout();}}/>
  </View>}
  {error&&<Text accessibilityRole="alert" style={styles.error}>{error}</Text>}
 </SectionScreen>;
}
const styles=StyleSheet.create({panel:{gap:16},title:{fontSize:22,color:colors.primary,fontWeight:'600'},text:{fontSize:16,lineHeight:24,color:colors.secondary},error:{fontSize:15,lineHeight:22,color:colors.error}});
