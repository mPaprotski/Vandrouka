import {useCallback} from 'react';
import {StyleSheet,Text,View} from 'react-native';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {usePrivateResource} from '../hooks/usePrivateResource';
import {progressPercent} from '../progress/presentation';
import {AppButton} from './ui/AppButton';
import {ErrorState,LoadingState} from './ui/ContentState';
import {colors} from './ui/theme';

export function ProgressPanel({owner,session}:{owner:number;session:SessionController}){
 const load=useCallback((signal:AbortSignal)=>session.withAccess(token=>api.getProgress(token,{signal}),owner),[owner,session]);
 const {state,retry}=usePrivateResource(load);
 if((state.status==='loading'||state.status==='idle'))return <LoadingState message="Загружаем ваш прогресс…" />;
 if(state.status==='error')return <ErrorState message={state.message} requestId={state.requestId} onRetry={retry} />;
 const p=state.data,percent=progressPercent(p);
 return <View style={styles.panel}>
  <Text accessibilityRole="header" style={styles.title}>Уровень {p.level}</Text>
  <Text style={styles.xp}>{p.total_xp} XP</Text>
  <View accessibilityRole="progressbar" accessibilityLabel="Прогресс текущего уровня" accessibilityValue={{min:0,max:100,now:percent,text:p.xp_to_next_level===null?'Максимальный уровень':`До следующего уровня ${p.xp_to_next_level} XP`}} style={styles.track}>
   <View style={[styles.fill,{width:`${percent}%`}]} />
  </View>
  <Text style={styles.note}>{p.next_level_xp===null?'Максимальный уровень достигнут. Продолжайте открывать места и накапливать XP.':`До уровня ${p.level+1}: ${p.xp_to_next_level} XP`}</Text>
  <AppButton title="Обновить прогресс" variant="secondary" onPress={retry} />
 </View>;
}
const styles=StyleSheet.create({panel:{gap:12,padding:20,borderRadius:18,borderWidth:1,borderColor:colors.border,backgroundColor:colors.surface},title:{fontSize:24,fontWeight:'700',color:colors.primary},xp:{fontSize:30,fontWeight:'600',color:colors.primary},note:{fontSize:15,lineHeight:23,color:colors.secondary},track:{height:12,overflow:'hidden',borderRadius:6,backgroundColor:colors.border},fill:{height:'100%',backgroundColor:colors.primary,borderRadius:6}});
