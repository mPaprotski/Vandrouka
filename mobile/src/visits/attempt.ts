import {decodeUploadedPhoto} from '../api/client.ts';
import type {UploadedPhoto,VisitInput} from '../api/client.ts';
export type Attempt={key:string;photo:UploadedPhoto;input:VisitInput};
export type AttemptStorage={read():Promise<Attempt|null>;write(attempt:Attempt|null,expectedKey?:string):Promise<void>};
export function decodeAttempt(value:unknown,placeId:number):Attempt{
 if(!value||typeof value!=='object')throw new Error('invalid attempt');
 const a=value as Attempt,l=a.input?.location;
 const uuid=/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i;
 const photo=decodeUploadedPhoto(a.photo);
 if(typeof a.key!=='string'||!uuid.test(a.key)||photo.place_id!==placeId||a.input?.photo_id!==photo.id||!l||!Number.isFinite(l.lat)||Math.abs(l.lat)>90||!Number.isFinite(l.lon)||Math.abs(l.lon)>180||!Number.isFinite(l.accuracy_m)||l.accuracy_m<0||typeof l.observed_at!=='string'||!/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(l.observed_at)||!Number.isFinite(Date.parse(l.observed_at)))throw new Error('invalid attempt');
 return {key:a.key,photo,input:{photo_id:photo.id,location:{lat:l.lat,lon:l.lon,accuracy_m:l.accuracy_m,observed_at:l.observed_at}}};
}

const queues=new Map<string,Promise<unknown>>();
async function serialized<T>(key:string,work:()=>Promise<T>):Promise<T>{
 const before=queues.get(key)??Promise.resolve(),result=before.catch(()=>{}).then(work);queues.set(key,result);
 try{return await result;}finally{if(queues.get(key)===result)queues.delete(key);}
}
export function createAttemptStorage(origin:string,owner:number,place:number,key:Promise<string>,store:{get(key:string):Promise<string|null>;set(key:string,value:string|null):Promise<void>}):AttemptStorage{
 const decode=(raw:string|null)=>{
  if(raw===null)return null;if(raw.length>4096)throw new Error('invalid attempt');
  const record=JSON.parse(raw);
  if(record.version!==1||record.origin!==origin||record.owner!==owner||record.place!==place)throw new Error('invalid attempt');
  return decodeAttempt(record.attempt,place);
 };
 return {
  async read(){const resolved=await key;return serialized(resolved,async()=>decode(await store.get(resolved)));},
  async write(attempt,expectedKey){
   const resolved=await key;
   await serialized(resolved,async()=>{
    const old=decode(await store.get(resolved));
    if(attempt===null){if(old&&old.key!==expectedKey)throw new Error('different pending attempt');await store.set(resolved,null);return;}
    const normalized=decodeAttempt(attempt,place);
    if(old&&JSON.stringify(old)!==JSON.stringify(normalized))throw new Error('different pending attempt');
    const record=JSON.stringify({version:1,origin,owner,place,attempt:normalized});
    if(record.length>4096)throw new Error('invalid attempt');await store.set(resolved,record);
   });
  },
 };
}
