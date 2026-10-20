import * as Crypto from 'expo-crypto';
import { getApiBaseUrl } from '../../config/api';
import { secureAttemptStorage } from '../../visits/attemptStorage';
import { CameraView, useCameraPermissions } from 'expo-camera';
import { router, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { AppState, Image, Linking, Platform, StyleSheet, Text, View } from 'react-native';
import { api } from '../../api/client';
import { useSession } from '../../auth/SessionProvider';
import type { SessionController } from '../../auth/session';
import { SectionScreen, sectionStyles } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { ErrorState, LoadingState } from '../../components/ui/ContentState';
import { colors } from '../../components/ui/theme';
import { useApiResource } from '../../hooks/useApiResource';
import { locateForPhoto, removeLocalPhoto, takeLocalPhoto } from '../../visits/native';
import { choosePictureSize, createPhotoController } from '../../visits/photo';

function PhotoDraft({placeId,owner,session}:{placeId:number;owner:number;session:SessionController}){
 const [flow]=useState(()=>createPhotoController({
  locate:locateForPhoto,
  upload:(photo,signal)=>session.withAccess(token=>api.uploadPhoto(placeId,photo,token,{signal}),owner),
  removeRemote:id=>session.withAccess(token=>api.discardPhoto(id,token),owner),
  removeLocal:removeLocalPhoto,
  confirmation:{
   key:Crypto.randomUUID,
   storage:secureAttemptStorage(getApiBaseUrl(),owner,placeId),
   send:(key,input,signal)=>session.withAccess(token=>api.confirmVisit(placeId,key,input,token,{signal}),owner),
  },
 }));
 const state=useSyncExternalStore(flow.subscribe,flow.getSnapshot,flow.getSnapshot);
 const [permission,requestPermission]=useCameraPermissions();
 const [focused,setFocused]=useState(false);
 const [active,setActive]=useState(AppState.currentState==='active');
 const [size,setSize]=useState<string>();
 const [ready,setReady]=useState(false);
 const [cameraError,setCameraError]=useState<string>();
 const [asking,setAsking]=useState(false);
 const [cameraKey,setCameraKey]=useState(0);
 const camera=useRef<CameraView>(null);
 const cameraEpoch=useRef(0);
 useFocusEffect(useCallback(()=>{
  setFocused(true);
  return()=>{setFocused(false);setReady(false);cameraEpoch.current++;flow.interrupt();};
 },[flow]));
 useEffect(()=>{
  void flow.restore();
  const listener=AppState.addEventListener('change',next=>{
   setActive(next==='active');if(next!=='active'){cameraEpoch.current++;setReady(false);flow.interrupt();}
  });
  return()=>{listener.remove();flow.dispose();};
 },[flow]);
 const showCamera=focused&&active&&permission?.granted&&(state.status==='camera'||state.status==='capturing');
 const cameraReady=async()=>{
  const epoch=++cameraEpoch.current;
  setCameraError(undefined);
  try{
   const sizes=await camera.current?.getAvailablePictureSizesAsync();
   if(epoch!==cameraEpoch.current)return;
   const selected=choosePictureSize(sizes??[]);
   if(!selected){setCameraError('Камера не предложила подходящий размер снимка. Попробуйте открыть её заново.');return;}
   setSize(selected);setReady(true);
  }catch{if(epoch===cameraEpoch.current)setCameraError('Не удалось подготовить камеру. Попробуйте ещё раз.');}
 };
 const grant=async()=>{
  if(asking)return;setAsking(true);setCameraError(undefined);
  try{await requestPermission();}catch{setCameraError('Не удалось запросить доступ к камере. Попробуйте ещё раз.');}finally{setAsking(false);}
 };
 const busy=['capturing','locating','uploading','confirmLocating','confirming'].includes(state.status);
 const unresolved=state.status==='uncertain'||state.status==='confirming';
 return <View style={styles.content}>
  {state.status==='restoring'&&<LoadingState message="Проверяем незавершённые посещения…" />}
  {state.status==='storage_error'&&<ErrorState message={state.message??'Защищённое хранилище недоступно.'} onRetry={()=>void flow.restore()} />}
  {!permission&&state.status==='camera'&&<LoadingState message="Проверяем доступ к камере…" />}
  {permission&&!permission.granted&&state.status==='camera'&&<>
   <Text style={sectionStyles.note}>Разрешите камеру, чтобы сфотографировать место. Снимок не сохраняется в галерею.</Text>
   {permission.canAskAgain?<AppButton title="Разрешить камеру" loading={asking} onPress={()=>void grant()} />:<AppButton title="Открыть настройки камеры" onPress={()=>void Linking.openSettings().catch(()=>setCameraError('Не удалось открыть настройки. Откройте их вручную.'))} />}
  </>}
  {showCamera&&<CameraView key={cameraKey} ref={camera} style={styles.image} facing="back" mode="picture" pictureSize={size} onCameraReady={()=>void cameraReady()} onMountError={()=>{setReady(false);setCameraError('Камера недоступна. Закройте другие приложения с камерой и попробуйте снова.');}} />}
  {state.photo&&<Image source={{uri:state.photo.uri}} style={styles.image} resizeMode="contain" accessibilityLabel="Предпросмотр снимка места" />}
  {!active&&<Text style={sectionStyles.note}>Вернитесь в приложение, чтобы продолжить.</Text>}
  {cameraError&&<ErrorState message={cameraError} onRetry={()=>{setCameraError(undefined);flow.retake();setReady(false);setCameraKey(v=>v+1);}} />}
  {state.message&&state.status!=='storage_error'&&<View accessibilityLiveRegion="polite"><Text style={styles.error}>{state.message}</Text>{state.requestId&&<Text style={sectionStyles.note}>Код обращения: {state.requestId}</Text>}</View>}
  {permission?.granted&&state.status==='camera'&&<AppButton title="Сделать снимок" disabled={!ready||!active||!focused} onPress={()=>void flow.capture(async()=>{if(!camera.current)throw new Error('camera unavailable');return takeLocalPhoto(camera.current);})} />}
  {state.status==='capturing'&&<LoadingState message="Делаем снимок…" />}
  {state.status==='preview'&&<AppButton title="Загрузить фото" disabled={!active||!focused} onPress={()=>void flow.upload()} />}
  {state.status==='locating'&&<LoadingState message="Получаем свежую геопозицию с точностью до 30 м…" />}
  {state.status==='uploading'&&<LoadingState message="Загружаем приватное фото…" />}
  {state.status==='uploaded'&&<View accessibilityLiveRegion="polite"><Text style={styles.success}>Фото загружено</Text><Text style={sectionStyles.note}>Фото готово. Подтвердите посещение, когда будете рядом с местом.</Text></View>}
  {(state.status==='uploaded'||state.status==='rejected')&&<AppButton title={state.status==='rejected'?'Повторить со свежим GPS':'Подтвердить посещение'} disabled={!active||!focused} onPress={()=>void flow.confirm()} />}
  {state.status==='confirmLocating'&&<LoadingState message="Обновляем геопозицию перед подтверждением…" />}
  {state.status==='confirming'&&<LoadingState message="Подтверждаем посещение…" />}
  {state.status==='uncertain'&&<><Text style={sectionStyles.note}>Результат пока неизвестен. Проверка повторит тот же запрос без повторного начисления XP.</Text><AppButton title="Проверить результат" disabled={!active||!focused} onPress={()=>void flow.confirm()} /></>}
  {state.status==='confirmed'&&state.result&&<View accessibilityLiveRegion="polite"><Text style={styles.success}>{state.result.xp_awarded===100?'Место открыто — +100 XP':'Повторное посещение — 0 XP'}</Text><Text style={sectionStyles.note}>Посещение сохранено. Фото сохранено вместе с посещением.</Text></View>}
  {state.status==='rejected'&&state.result?.error&&<View accessibilityLiveRegion="polite"><Text style={styles.error}>Посещение не подтверждено</Text><Text style={sectionStyles.note}>{state.result.error.message}. XP не начислен.</Text></View>}
  {(state.photo||state.uploaded)&&state.status!=='confirmed'&&<AppButton title="Переснять" variant="secondary" disabled={busy||unresolved} onPress={()=>{setReady(false);cameraEpoch.current++;flow.retake();}} />}
  <AppButton title={busy?'Отменить действие':'Вернуться к месту'} variant="secondary" onPress={()=>{if(busy)flow.interrupt();else router.back();}} />
  {['preview','uploaded','rejected'].includes(state.status)&&<AppButton title="Настройки геолокации" variant="secondary" onPress={()=>void Linking.openSettings().catch(()=>{})} />}
 </View>;
}
export default function CaptureScreen(){
 const {id}=useLocalSearchParams<{id:string}>();const {state:session,controller}=useSession();
 const load=useCallback((signal:AbortSignal)=>api.getPlace(id??'',{signal}),[id]);
 const {state,retry,data}=useApiResource(load);
 // A focus refresh must not discard a private draft while the same place reloads.
 const place=state.status==='success'?state.data:state.status==='loading'&&data?.id===Number(id)?data:undefined;
 return <SectionScreen title={place?place.name:'Фото места'} description="Сфотографируйте место и подтвердите посещение рядом с ним.">
  {state.status==='loading'&&!place&&<LoadingState message="Загружаем место…" />}
  {state.status==='error'&&<ErrorState message={state.message} requestId={state.requestId} onRetry={retry} />}
  {session.status==='loading'&&<LoadingState message="Проверяем сессию…" />}
  {session.status==='error'&&<ErrorState message={session.message} requestId={session.requestId} onRetry={()=>void controller.resume()} />}
  {session.status==='guest'&&<><Text style={sectionStyles.note}>Войдите, чтобы сохранить личное фото места.</Text><AppButton title="Войти или зарегистрироваться" onPress={()=>router.push('/auth')} /></>}
  {Platform.OS==='web'&&<Text style={sectionStyles.note}>Съёмка доступна в Expo Go на телефоне.</Text>}
  {Platform.OS!=='web'&&session.status==='authenticated'&&place&&<PhotoDraft key={`${session.user.id}:${place.id}`} placeId={place.id} owner={session.user.id} session={controller} />}
 </SectionScreen>;
}
const styles=StyleSheet.create({content:{gap:16},image:{width:'100%',height:360,borderRadius:18,overflow:'hidden',backgroundColor:'#17231e'},error:{color:colors.error,fontSize:16,lineHeight:24},success:{color:colors.primary,fontSize:22,fontWeight:'700'}});
