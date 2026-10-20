import { ApiError, decodeAuthSession } from '../api/client.ts';
import type { AuthSession, AuthUser, Credentials } from '../api/client.ts';

export type SessionAPI = {
 login(input:Credentials):Promise<AuthSession>;
 register(input:Credentials):Promise<AuthSession>;
 refresh(token:string):Promise<AuthSession>;
 logout(token:string):Promise<unknown>;
 me(token:string):Promise<AuthUser>;
};
export type SessionStorage = {read():Promise<string|null>;write(value:string):Promise<void>};
export type SessionState =
 | {status:'loading'}
 | {status:'guest';message?:string}
 | {status:'authenticated';user:AuthUser}
 | {status:'error';message:string;requestId?:string};
const cancelled = () => new ApiError('cancelled','Действие отменено.');
const expired = () => new ApiError('http','Сессия завершилась. Войдите снова.',{status:401});
const unauthorized=(error:unknown)=>error instanceof ApiError&&error.status===401;
class StorageError extends Error {constructor(){super('Не удалось обновить защищённое хранилище. Повторите действие.');}}
const secureError=()=>new StorageError();

/** Native-free state machine; credentials stay in memory and encrypted storage only. */
export function createSessionController({api,storage,origin,now=Date.now}:{api:SessionAPI;storage:SessionStorage;origin:string;now?:()=>number}) {
 let state:SessionState={status:'loading'};
 let session:AuthSession|null=null;
 let pending:string[]=[];
 let epoch=0;
 let booted=false;
 let signingIn=false;
 let refreshFlight:{epoch:number;promise:Promise<void>}|undefined;
 let resumeFlight:Promise<void>|undefined;
 let storageQueue:Promise<unknown>=Promise.resolve();
 let flushFlight:Promise<void>|undefined;
 const listeners=new Set<()=>void>();
 const publish=(next:SessionState)=>{state=next;listeners.forEach(listener=>listener());};
 const failure=(error:unknown)=>({status:'error' as const,message:error instanceof ApiError||error instanceof StorageError?error.message:'Не удалось восстановить сессию. Повторите попытку.',requestId:error instanceof ApiError?error.requestId:undefined});
 function persist(expected?:number):Promise<void> {
  // Capture the record now; execute writes in order, including late refresh/logout races.
  const record=JSON.stringify({version:1,origin,session,pending});
  const write=storageQueue.then(async()=>{if(expected!==undefined&&expected!==epoch)return;try{await storage.write(record);}catch{throw secureError();}});
  storageQueue=write.catch(()=>{});return write;
 }
 async function flush():Promise<void>{
  if(flushFlight)return flushFlight;
  const work=(async()=>{while(pending.length){const token=pending[0];await api.logout(token);pending=pending.filter(value=>value!==token);await persist();}})();
  flushFlight=work;
  try{await work;}finally{if(flushFlight===work)flushFlight=undefined;}
 }
 async function revokeLate(result:AuthSession){
  if(!pending.includes(result.refresh_token))pending.push(result.refresh_token);
  try{await persist();}catch{/* Still attempt server revocation if native storage failed. */}
  try{await flush();}catch{/* Retry marker remains whenever native storage is available. */}
 }
 async function invalidate(message='Сессия завершилась. Войдите снова.'){
  epoch++;session=null;await persist();publish({status:'guest',message});
 }
 async function renew():Promise<void>{
  if(!session)throw expired();
  if(refreshFlight?.epoch===epoch)return refreshFlight.promise;
  const version=epoch,previous=session;
  const work=(async()=>{
   if(Date.parse(previous.refresh_expires_at)<=now()){await invalidate();throw expired();}
   try{
    const updated=await api.refresh(previous.refresh_token);
    if(version!==epoch){await revokeLate(updated);throw cancelled();}
    if(updated.user.id!==previous.user.id)throw new ApiError('invalid_response','Сервер вернул другую учётную запись.');
    session=updated;
    try{await persist(version);}catch(error){session=null;await revokeLate(updated);throw error;}
   }catch(error){
    if(version===epoch){
     if(unauthorized(error)){await invalidate();}
     else publish(failure(error));
    }
    throw error;
   }
  })();
  refreshFlight={epoch:version,promise:work};
  try{await work;}finally{if(refreshFlight?.promise===work)refreshFlight=undefined;}
 }
 async function withAccess<T>(operation:(token:string)=>Promise<T>,expectedOwner?:number):Promise<T>{
  const version=epoch;
  if(!session)throw expired();
  if(expectedOwner!==undefined&&session.user.id!==expectedOwner)throw cancelled();
  if(Date.parse(session.expires_at)<=now()+30_000)await renew();
  if(version!==epoch||!session)throw cancelled();
  const token=session.access_token;
  let result:T;
  try{result=await operation(token);}catch(error){
   if(version!==epoch)throw cancelled();
   if(!unauthorized(error))throw error;
   if(session?.access_token===token)await renew();
   if(version!==epoch||!session)throw cancelled();
   try{result=await operation(session.access_token);}catch(second){if(version===epoch&&unauthorized(second))await invalidate();throw second;}
  }
  if(version!==epoch)throw cancelled();
  return result;
 }
 async function restore(){
  const version=epoch;
  if(!booted){
   let raw:string|null;
   try{raw=await storage.read();}catch{throw secureError();}
   if(version!==epoch)return;
   if(raw){
    try{
     const record=JSON.parse(raw);
     if(record.version!==1||record.origin!==origin||!Array.isArray(record.pending)||record.pending.length>8||!record.pending.every((v:unknown)=>typeof v==='string'&&/^[A-Za-z0-9_-]{43}$/.test(v)))throw new Error('record');
     session=record.session===null?null:decodeAuthSession(record.session);pending=record.pending;
    }catch{session=null;pending=[];await persist(version);}
   }
   booted=true;
  }
  if(pending.length){
   try{await flush();}catch(error){if(!session){publish({status:'guest',message:'Вы вышли на этом устройстве. Отзыв на сервере завершится после подключения.'});return;}throw error;}
  }
  if(version!==epoch||signingIn)return;
  if(!session){publish({status:'guest'});return;}
  const user=await withAccess(token=>api.me(token));
  if(version!==epoch||!session)return;
  if(user.id!==session.user.id){await invalidate('Сессия не прошла проверку. Войдите снова.');return;}
  session={...session,user};await persist(version);
  if(version===epoch)publish({status:'authenticated',user});
 }
 async function resume():Promise<void>{
  if(signingIn)return;
  if(resumeFlight)return resumeFlight;
  const version=epoch;
  const work=restore().catch(error=>{if(version===epoch&&!(error instanceof ApiError&&error.kind==='cancelled'))publish(failure(error));});
  resumeFlight=work;
  try{await work;}finally{if(resumeFlight===work)resumeFlight=undefined;}
 }
 async function logout():Promise<void>{
  epoch++;const version=epoch;
  if(session&&!pending.includes(session.refresh_token))pending.push(session.refresh_token);
  session=null;booted=true;publish({status:'loading'});
  try{await persist(version);}catch(error){try{await flush();}catch{};publish(failure(error));throw error;}
  if(version!==epoch)return;
  publish({status:'guest',message:'Выход завершается…'});
  try{await flush();if(version===epoch)publish({status:'guest'});}catch{
   if(version===epoch)publish({status:'guest',message:'Вы вышли на этом устройстве. Отзыв на сервере завершится после подключения.'});
  }
 }
 async function signIn(mode:'login'|'register',input:Credentials):Promise<void>{
  if(signingIn)throw new Error('Дождитесь завершения входа.');
  signingIn=true;
  try{
   if(!booted){await storageQueue;await restore();}
   if(session)await logout();
   await flush();
   const version=++epoch;publish({status:'loading'});
   let result:AuthSession;
   try{result=await api[mode](input);}catch(error){if(version===epoch)publish({status:'guest'});throw error;}
   if(version!==epoch){await revokeLate(result);throw cancelled();}
   session=result;booted=true;
   try{await persist(version);}catch(error){session=null;await revokeLate(result);if(version===epoch)publish(failure(error));throw error;}
   if(version!==epoch)throw cancelled();
   publish({status:'authenticated',user:result.user});
  }finally{signingIn=false;}
 }
 return {
  getSnapshot:()=>state,
  subscribe:(listener:()=>void)=>{listeners.add(listener);return()=>{listeners.delete(listener);};},
  resume,signIn,logout,withAccess,
 };
}
export type SessionController=ReturnType<typeof createSessionController>;
