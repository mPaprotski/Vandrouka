import { ApiError } from '../api/client.ts';
import type { LocalPhoto, UploadedPhoto, VisitOutcome, VisitInput } from '../api/client.ts';
import type { Attempt,AttemptStorage } from './attempt.ts';
import type { PositionFix } from '../location/locate.ts';

/** Keep captures below backend pixel limits before encoding/upload. */
export function choosePictureSize(sizes: string[]): string | undefined {
 return sizes.map(size=>({size,parts:size.split('x').map(Number)}))
 .filter(({parts})=>parts.length===2)
 .filter(({parts:[w,h]})=>Number.isSafeInteger(w)&&Number.isSafeInteger(h)&&w>0&&h>0&&w<=8192&&h<=8192&&w*h<=4000000)
 .sort((a,b)=>b.parts[0]*b.parts[1]-a.parts[0]*a.parts[1])[0]?.size;
}
export function visitFixReady(fix:PositionFix,now=Date.now()):boolean {
 return Number.isFinite(fix.latitude)&&Math.abs(fix.latitude)<=90&&Number.isFinite(fix.longitude)&&Math.abs(fix.longitude)<=180&&fix.accuracy!==null&&Number.isFinite(fix.accuracy)&&fix.accuracy>=0&&fix.accuracy<=30&&Number.isFinite(fix.timestamp)&&now-fix.timestamp<=30000&&fix.timestamp-now<=5000;
}
export type PhotoState={status:'restoring'|'storage_error'|'camera'|'capturing'|'preview'|'locating'|'uploading'|'uploaded'|'confirmLocating'|'confirming'|'uncertain'|'confirmed'|'rejected'|'photoExpired';photo?:LocalPhoto;uploaded?:UploadedPhoto;fix?:PositionFix;result?:VisitOutcome;message?:string;requestId?:string};
type Dependencies={
 locate:(signal:AbortSignal)=>Promise<PositionFix>;
 upload:(photo:LocalPhoto,signal:AbortSignal)=>Promise<UploadedPhoto>;
 removeRemote:(id:string)=>Promise<void>;
 removeLocal:(uri:string)=>Promise<void>;
 confirmation?:{storage:AttemptStorage;key:()=>string;send:(key:string,input:VisitInput,signal:AbortSignal)=>Promise<VisitOutcome>};
};
/** A draft plus one durable idempotent confirmation. Uncertain/confirmed media is never discarded. */
export function createPhotoController(deps:Dependencies){
 let state:PhotoState={status:deps.confirmation?'restoring':'camera'},version=0,disposed=false,current:AbortController|undefined;
 let pending:Attempt|undefined,restoreFlight:Promise<void>|undefined;
 const protectedPhotos=new Set<string>(),listeners=new Set<()=>void>();
 const publish=(next:PhotoState)=>{if(!disposed){state=next;listeners.forEach(fn=>fn());}};
 const removeLocal=(uri:string)=>{void deps.removeLocal(uri).catch(()=>{});};
 const removeRemote=(id:string)=>{if(!protectedPhotos.has(id))void deps.removeRemote(id).catch(()=>{});};
 const valid=(v:number)=>!disposed&&v===version;
 const errorState=(error:unknown)=>({message:error instanceof Error?error.message:'Не удалось выполнить действие.',requestId:error instanceof ApiError?error.requestId:undefined});
 async function restore(){
  if(disposed||!deps.confirmation||!['restoring','storage_error'].includes(state.status))return;
  if(restoreFlight)return restoreFlight;
  publish({status:'restoring'});
  const work=(async()=>{
   try{
    const attempt=await deps.confirmation!.storage.read();if(disposed)return;
    if(attempt){pending=attempt;protectedPhotos.add(attempt.photo.id);publish({status:'uncertain',uploaded:attempt.photo,message:'Есть незавершённое подтверждение. Проверьте его результат — повторной награды не будет.'});}
    else publish({status:'camera'});
   }catch{if(!disposed)publish({status:'storage_error',message:'Не удалось прочитать защищённую попытку. Повторите проверку хранилища.'});}
  })();restoreFlight=work;try{await work;}finally{if(restoreFlight===work)restoreFlight=undefined;}
 }
 function interrupt(){
  version++;current?.abort();current=undefined;
  if(disposed)return;
  if(state.status==='confirming'){publish({...state,status:'uncertain',message:'Отправка прервана. Сервер мог сохранить посещение. Проверьте результат.'});}
  else if(state.status==='confirmLocating'){publish({...state,status:'uploaded',message:'Получение GPS прервано. Попробуйте ещё раз.'});}
  else if(['capturing','locating','uploading'].includes(state.status))publish({status:state.photo?'preview':'camera',photo:state.photo,message:'Действие прервано. Повторите, когда будете готовы.'});
 }
 async function capture(take:()=>Promise<LocalPhoto>){
  if(disposed||state.status!=='camera')return;
  const v=++version;publish({status:'capturing'});
  try{const photo=await take();if(!valid(v)){removeLocal(photo.uri);return;}publish({status:'preview',photo});}
  catch{if(valid(v))publish({status:'camera',message:'Не удалось сделать снимок. Попробуйте ещё раз.'});}
 }
 async function upload(){
  if(disposed||state.status!=='preview'||!state.photo)return;
  const photo=state.photo,v=++version,abort=new AbortController();current=abort;publish({status:'locating',photo});
  try{
   const fix=await deps.locate(abort.signal);if(!valid(v))return;
   if(!visitFixReady(fix))throw new Error('Нужна свежая геопозиция с точностью до 30 м. Попробуйте на открытом месте.');
   publish({status:'uploading',photo});const uploaded=await deps.upload(photo,abort.signal);
   if(!valid(v)){removeRemote(uploaded.id);return;}publish({status:'uploaded',photo,uploaded,fix});
  }catch(error){if(valid(v)&&!abort.signal.aborted)publish({status:'preview',photo,...errorState(error)});}
  finally{if(current===abort)current=undefined;}
 }
 async function confirm(){
  const confirmation=deps.confirmation;
  if(disposed||!confirmation||current||!['uploaded','rejected','uncertain'].includes(state.status)||!state.uploaded)return;
  const v=++version,abort=new AbortController();current=abort;
  const uploaded=state.uploaded,photo=state.photo;
  try{
   if(!pending){
    publish({status:'confirmLocating',photo,uploaded});
    const fix=await deps.locate(abort.signal);if(!valid(v))return;
    if(!visitFixReady(fix))throw new Error('Нужна свежая геопозиция с точностью до 30 м. Попробуйте на открытом месте.');
    pending={key:confirmation.key(),photo:uploaded,input:{photo_id:uploaded.id,location:{lat:fix.latitude,lon:fix.longitude,accuracy_m:fix.accuracy!,observed_at:new Date(fix.timestamp).toISOString()}}};
    protectedPhotos.add(uploaded.id);
   }
   const attempt=pending;
   publish({status:'confirming',photo,uploaded});
   // Save before every send: a failed/ambiguous write never permits a network request.
   try{await confirmation.storage.write(attempt);}catch{throw new Error('Не удалось сохранить попытку защищённо. Запрос не отправлен; повторите проверку результата.');}
   if(!valid(v))return;
   const result=await confirmation.send(attempt.key,attempt.input,abort.signal);
   if(!valid(v))return;
   // Never allow an invalid/mismatched transport response to erase the durable request.
   if(result.visit.photo_id.toLowerCase()!==uploaded.id.toLowerCase()||result.visit.place_id!==uploaded.place_id)throw new Error('Ответ не соответствует посещению. Повторите проверку результата.');
   try{await confirmation.storage.write(null,attempt.key);}catch{throw new Error('Ответ получен, но не удалось завершить сохранённую попытку. Повторите проверку результата.');}
   if(!valid(v))return;
   pending=undefined;
   if(result.visit.status==='rejected')protectedPhotos.delete(uploaded.id);
   publish({status:result.visit.status==='confirmed'?'confirmed':'rejected',photo,uploaded,result});
  }catch(error){
   if(valid(v)&&!abort.signal.aborted){
    // These domain errors occur after the server's idempotency lookup. The key has
    // no stored outcome and the photo cannot confirm this request. Keep remote media.
    if(pending&&error instanceof ApiError&&((error.status===404&&error.code==='not_found')||(error.status===409&&error.code==='photo_already_used'))){
     try{
      await confirmation.storage.write(null,pending.key);
      if(valid(v)){pending=undefined;publish({status:'photoExpired',photo,uploaded,message:'Это фото больше нельзя использовать для нового подтверждения. Сделайте новый снимок.'});}
     }catch{if(valid(v))publish({status:'uncertain',photo,uploaded,message:'Не удалось завершить сохранённую попытку. Повторите проверку результата.'});}
    }else publish({status:pending?'uncertain':'uploaded',photo,uploaded,...errorState(error)});
   }
  }
  finally{if(current===abort)current=undefined;}
 }
 function retake(){
  if(pending||['restoring','storage_error','confirmLocating','confirming','uncertain'].includes(state.status))return;
  interrupt();if(state.photo)removeLocal(state.photo.uri);if(state.uploaded)removeRemote(state.uploaded.id);publish({status:'camera'});
 }
 function dispose(){if(disposed)return;interrupt();if(state.photo)removeLocal(state.photo.uri);if(state.uploaded)removeRemote(state.uploaded.id);disposed=true;listeners.clear();}
 return {getSnapshot:()=>state,subscribe:(fn:()=>void)=>{listeners.add(fn);return()=>{listeners.delete(fn);};},capture,upload,confirm,restore,interrupt,retake,dispose};
}
