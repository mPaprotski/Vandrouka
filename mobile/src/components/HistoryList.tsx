import {VisitDeletion} from './VisitDeletion';
import {useCallback,useEffect,useState,useSyncExternalStore} from 'react';
import {AppState,FlatList,StyleSheet,Text,View} from 'react-native';
import {useFocusEffect,router} from 'expo-router';
import {SafeAreaView} from 'react-native-safe-area-context';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {createHistoryController} from '../history/controller';
import {historyDate} from '../history/photo';
import {PrivatePhoto} from './PrivatePhoto';
import {AppButton} from './ui/AppButton';
import {EmptyState,ErrorState,LoadingState} from './ui/ContentState';
import {colors} from './ui/theme';

export function HistoryList({owner,session}:{owner:number;session:SessionController}){
 const [controller]=useState(()=>createHistoryController((cursor,signal)=>session.withAccess(token=>api.listHistory(token,cursor,{signal}),owner)));
 const state=useSyncExternalStore(controller.subscribe,controller.getSnapshot,controller.getSnapshot);
 const [selected,setSelected]=useState<number|null>(null);
 useFocusEffect(useCallback(()=>{
  let active=false;
  const update=(value:string|null|undefined)=>{const next=value==='active';if(next===active)return;active=next;setSelected(null);if(next)void controller.refresh();else controller.suspend();};
  update(AppState.currentState);
  const listener=AppState.addEventListener('change',update);
  return()=>{listener.remove();setSelected(null);controller.suspend();};
 },[controller]));
 useEffect(()=>()=>controller.suspend(),[controller]);
 return <SafeAreaView edges={['left','right']} style={styles.screen}><FlatList
  data={state.items} keyExtractor={item=>String(item.id)} contentContainerStyle={styles.list}
  initialNumToRender={6} maxToRenderPerBatch={6} windowSize={5}
  ListHeaderComponent={<View style={styles.header}><Text style={styles.title}>Ваши открытия</Text><Text style={styles.note}>Подтверждённые посещения и воспоминания о прогулках.</Text><AppButton title="Обновить историю" variant="secondary" onPress={()=>{setSelected(null);void controller.refresh();}}/></View>}
  ListEmptyComponent={(state.status==='loading'||state.status==='idle')?<LoadingState message="Загружаем посещения…"/>:state.status==='error'?<ErrorState message={state.message} requestId={state.requestId} onRetry={()=>void controller.refresh()}/>:<EmptyState title="Пока нет открытий" description="Посетите место и подтвердите посещение с фотографией." action={<AppButton title="Исследовать город" onPress={()=>router.navigate('/explore')}/>}/>}
  renderItem={({item})=><View style={styles.card}><Text style={styles.name}>{item.place_name}</Text><Text style={styles.note}>{historyDate(item.confirmed_at,item.timezone)}</Text><Text style={styles.xp}>{item.xp_awarded?`+${item.xp_awarded} XP`:'Повторное посещение · 0 XP'}</Text>
   {item.place_active?<AppButton title="О месте" variant="secondary" onPress={()=>router.push({pathname:'/places/[id]',params:{id:item.place_id}})}/>:<Text style={styles.note}>Место больше недоступно в каталоге.</Text>}
   {item.photo_id?<><AppButton title={selected===item.id?'Скрыть фото':'Показать фото'} variant="secondary" onPress={()=>setSelected(selected===item.id?null:item.id)}/>{selected===item.id?<PrivatePhoto key={`${owner}:${item.photo_id}`} id={item.photo_id} style={styles.photo} owner={owner} session={session}/>:null}</>:<Text style={styles.note}>Фото удалено</Text>}
   <VisitDeletion id={item.id} photo={item.photo_id} owner={owner} session={session} onRemoved={()=>{setSelected(null);void controller.refresh();}}/>
  </View>}
  ListFooterComponent={state.next?<View style={styles.header}>{state.moreError?<Text accessibilityRole="alert" style={styles.note}>{state.moreError}</Text>:null}<AppButton title="Загрузить ещё" loading={state.loadingMore} onPress={()=>void controller.more()}/></View>:undefined}
 /></SafeAreaView>;
}
const styles=StyleSheet.create({screen:{flex:1,backgroundColor:colors.background},list:{padding:20,gap:16,paddingBottom:32},header:{gap:16},title:{fontSize:28,fontWeight:'700',color:colors.primary},name:{fontSize:21,fontWeight:'600',color:colors.primary},note:{fontSize:16,lineHeight:24,color:colors.secondary},xp:{fontSize:18,color:colors.primary,fontWeight:'600'},card:{padding:20,borderRadius:20,borderWidth:1,borderColor:colors.border,backgroundColor:colors.surface,gap:12},photo:{width:'100%',height:240}});
