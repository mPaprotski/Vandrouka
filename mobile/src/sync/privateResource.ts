import {ApiError} from '../api/client.ts';
export type PrivateState<T>={status:'idle'}|{status:'loading'}|{status:'success';data:T}|{status:'error';message:string;requestId?:string};
/** No stale cache: only a focused foreground screen may publish private data. */
export function createPrivateResource<T>(load:(signal:AbortSignal)=>Promise<T>){
 let state:PrivateState<T>={status:'idle'},focused=false,active=false,generation=0,current:AbortController|undefined;
 const listeners=new Set<()=>void>();
 const publish=(next:PrivateState<T>)=>{state=next;listeners.forEach(fn=>fn());};
 function stop(){generation++;current?.abort();current=undefined;publish({status:'idle'});}
 async function refresh(){
  stop();if(!focused||!active)return;
  const version=generation,abort=new AbortController();current=abort;publish({status:'loading'});
  try{const data=await load(abort.signal);if(version===generation&&!abort.signal.aborted)publish({status:'success',data});}
  catch(error){if(version===generation&&!abort.signal.aborted)publish({status:'error',message:error instanceof Error?error.message:'Не удалось загрузить данные.',requestId:error instanceof ApiError?error.requestId:undefined});}
  finally{if(current===abort)current=undefined;}
 }
 return {
  getSnapshot:()=>state,
  subscribe:(fn:()=>void)=>{listeners.add(fn);return()=>{listeners.delete(fn);};},
  setFocused(value:boolean){if(focused===value)return;focused=value;if(value)void refresh();else stop();},
  setActive(value:boolean){if(active===value)return;active=value;if(value&&focused)void refresh();else if(!value)stop();},
  refresh,
 };
}
