import {useCallback} from 'react';
import {StyleSheet,Text,View} from 'react-native';
import {router} from 'expo-router';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {usePrivateResource} from '../hooks/usePrivateResource';
import {AppButton} from './ui/AppButton';
import {ErrorState,LoadingState} from './ui/ContentState';
import {colors} from './ui/theme';
export function ExplorationPanel({owner,session}:{owner:number;session:SessionController}){
 const load=useCallback((signal:AbortSignal)=>session.withAccess(token=>api.getExploration(token,{signal}),owner),[owner,session]);
 const {state,retry}=usePrivateResource(load);
 if(state.status==='idle'||state.status==='loading')return <LoadingState message="Загружаем достижения и коллекции…"/>;
 if(state.status==='error')return <ErrorState message={state.message} requestId={state.requestId} onRetry={retry}/>;
 return <View style={styles.group}>
  <Text accessibilityRole="header" style={styles.title}>Ваши достижения</Text>
  <Text style={styles.note}>Уникальных открытых мест: {state.data.discovered_places}. Достижения не дают дополнительный XP.</Text>
  <Text style={styles.note}>Открытия учитываются за всё время, даже если запись скрыта из истории.</Text>
  {state.data.achievements.map(a=><View key={a.code} style={styles.card}>
   <Text style={styles.name}>{a.earned_at?'✓ ':''}{a.name}</Text>
   <Text style={styles.note}>{a.earned_at?'Получено':`${a.progress} из ${a.target} мест`}</Text>
  </View>)}
  <Text accessibilityRole="header" style={styles.title}>Коллекции</Text>
  {state.data.collections.map(c=><View key={c.code} style={styles.card}>
   <Text style={styles.name}>{c.name}</Text><Text style={styles.note}>{c.completed_at?'✓ Коллекция собрана':`${c.visited} из ${c.total} мест`}</Text>
   {c.places.map(p=><View key={p.slug} style={styles.group}>
    <Text style={styles.note}>{p.visited?'✓':'○'} {p.name}</Text>
    {p.available&&p.place_id!==null?<AppButton title="О месте" variant="secondary" onPress={()=>router.push({pathname:'/places/[id]',params:{id:p.place_id!}})}/>:<Text style={styles.note}>Место временно недоступно в каталоге.</Text>}
   </View>)}
  </View>)}
  <AppButton title="Обновить достижения и коллекции" variant="secondary" onPress={retry}/>
 </View>;
}
const styles=StyleSheet.create({group:{gap:12},title:{fontSize:23,fontWeight:'700',color:colors.primary},name:{fontSize:19,fontWeight:'600',color:colors.primary},note:{fontSize:15,lineHeight:23,color:colors.secondary},card:{padding:18,gap:12,borderWidth:1,borderColor:colors.border,borderRadius:18,backgroundColor:colors.surface}});
