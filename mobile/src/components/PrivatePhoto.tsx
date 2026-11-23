import {useCallback,useState} from 'react';
import {Image} from 'react-native';
import type {ImageStyle,StyleProp} from 'react-native';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {privatePhotoDataUri} from '../history/photo';
import {usePrivateResource} from '../hooks/usePrivateResource';
import {ErrorState,LoadingState} from './ui/ContentState';

export function PrivatePhoto({id,owner,session,style,onLoad,onError}:{id:string;owner:number;session:SessionController;style:StyleProp<ImageStyle>;onLoad?:()=>void;onError?:()=>void}){
 const load=useCallback(async(signal:AbortSignal)=>privatePhotoDataUri(await session.withAccess(token=>api.getPrivatePhoto(id,token,{signal}),owner),signal),[id,owner,session]);
 const {state,retry}=usePrivateResource(load);
 const [failed,setFailed]=useState(false);
 if(state.status==='loading'||state.status==='idle')return <LoadingState message="Загружаем личное фото…"/>;
 if(state.status==='error')return <ErrorState message={state.message} requestId={state.requestId} onRetry={retry}/>;
 if(failed)return <ErrorState message="Не удалось показать фото." onRetry={()=>{setFailed(false);retry();}}/>;
 return <Image accessibilityLabel="Фото вашего посещения" source={{uri:state.data}} style={style} resizeMode="contain" resizeMethod="resize" onLoad={onLoad} onError={()=>{setFailed(true);onError?.();}}/>;
}
