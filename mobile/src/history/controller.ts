import {ApiError} from '../api/client.ts';
import type {HistoryPage,HistoryItem} from '../api/client.ts';
export type HistoryState={status:'idle'|'loading'|'ready'|'error';items:HistoryItem[];next:string|null;loadingMore:boolean;message?:string;requestId?:string;moreError?:string};
export function createHistoryController(load:(cursor:string|undefined,signal:AbortSignal)=>Promise<HistoryPage>){
 let state:HistoryState={status:'idle',items:[],next:null,loadingMore:false},generation=0,current:AbortController|undefined;
 const listeners=new Set<()=>void>();
 const publish=(next:HistoryState)=>{state=next;listeners.forEach(fn=>fn());};
 const error=(e:unknown)=>({message:e instanceof Error?e.message:'Не удалось загрузить историю.',requestId:e instanceof ApiError?e.requestId:undefined});
 function suspend(){generation++;current?.abort();current=undefined;publish({status:'idle',items:[],next:null,loadingMore:false});}
 async function refresh(){
  suspend();const version=generation,abort=new AbortController();current=abort;publish({...state,status:'loading'});
  try{const page=await load(undefined,abort.signal);if(version===generation&&!abort.signal.aborted)publish({status:'ready',items:page.visits,next:page.next_cursor,loadingMore:false});}
  catch(e){if(version===generation&&!abort.signal.aborted)publish({status:'error',items:[],next:null,loadingMore:false,...error(e)});}
  finally{if(current===abort)current=undefined;}
 }
 async function more(){
  if(current||state.status!=='ready'||!state.next)return;
  const cursor=state.next,version=generation,abort=new AbortController();current=abort;publish({...state,loadingMore:true,moreError:undefined});
  try{
   const page=await load(cursor,abort.signal);if(version!==generation||abort.signal.aborted)return;
   if(page.next_cursor===cursor)throw new ApiError('invalid_response','Сервер повторил страницу истории. Обновите историю.');
   const ids=new Set(state.items.map(item=>item.id));publish({status:'ready',items:[...state.items,...page.visits.filter(item=>!ids.has(item.id))],next:page.next_cursor,loadingMore:false});
  }catch(e){if(version===generation&&!abort.signal.aborted)publish({...state,loadingMore:false,moreError:error(e).message,requestId:error(e).requestId});}
  finally{if(current===abort)current=undefined;}
 }
 return {getSnapshot:()=>state,subscribe:(fn:()=>void)=>{listeners.add(fn);return()=>{listeners.delete(fn);};},refresh,more,suspend};
}
