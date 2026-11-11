import {useCallback} from 'react';
import {api} from '../api/client';
import type {SessionController} from '../auth/session';
import {loadVisitedPlaces} from '../exploration/visited';
import {usePrivateResource} from './usePrivateResource';
type State={status:'loading'|'unknown'}|{status:'ready';ids:ReadonlySet<number>}|{status:'error';message:string};
export function useVisitedPlaces(owner:number|null,session:SessionController){
 const load=useCallback((signal:AbortSignal)=>owner===null?Promise.resolve(null):loadVisitedPlaces((cursor,signal)=>session.withAccess(token=>api.listHistory(token,cursor,{signal}),owner),signal),[owner,session]);
 const {state,retry}=usePrivateResource(load);
 const result:State=owner===null?{status:'unknown'}:state.status==='success'&&state.data!==null?{status:'ready',ids:state.data}:state.status==='error'?{status:'error',message:state.message}:{status:state.status==='loading'?'loading':'unknown'};
 return {state:result,retry};
}
