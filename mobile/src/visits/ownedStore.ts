type Store={get(key:string):Promise<string|null>;set(key:string,value:string|null):Promise<void>};
const queues=new Map<string,Promise<unknown>>();
async function serialize<T>(key:string,work:()=>Promise<T>){const pending=(queues.get(key)??Promise.resolve()).catch(()=>{}).then(work);queues.set(key,pending);try{return await pending;}finally{if(queues.get(key)===pending)queues.delete(key);}}
/** Encrypted owner-scoped index makes pending GPS records removable without enumerating SecureStore. */
export function createOwnedStore(prefix:string,store:Store){
 const indexKey=prefix+'.index';
 const decode=async()=>{const raw=await store.get(indexKey);if(raw===null)return {version:1,deleted:false,places:[] as number[]};if(raw.length>65536)throw Error('Некорректный индекс локальных посещений.');const value=JSON.parse(raw);if(value.version!==1||typeof value.deleted!=='boolean'||!Array.isArray(value.places)||value.places.length>4096||!value.places.every((id:unknown)=>Number.isSafeInteger(id)&&Number(id)>0))throw Error('Некорректный индекс локальных посещений.');return value as {version:number;deleted:boolean;places:number[]};};
 const place=(key:string)=>{if(!key.startsWith(prefix+'.'))throw Error('Другой владелец хранилища.');const id=Number(key.slice(prefix.length+1));if(!Number.isSafeInteger(id)||id<1||String(id)!==key.slice(prefix.length+1))throw Error('Некорректный ключ посещения.');return id;};
 const save=(index:Awaited<ReturnType<typeof decode>>)=>store.set(indexKey,JSON.stringify(index));
 return {
  get:(key:string)=>serialize(indexKey,async()=>{const id=place(key),index=await decode();if(index.deleted)throw Error('Аккаунт удалён.');const raw=await store.get(key);if(raw!==null&&!index.places.includes(id)){if(index.places.length>=4096)throw Error('Слишком много локальных посещений.');index.places.push(id);await save(index);}return raw;}),
  set:(key:string,value:string|null)=>serialize(indexKey,async()=>{const id=place(key),index=await decode();if(index.deleted)throw Error('Аккаунт удалён.');if(value!==null&&!index.places.includes(id)){if(index.places.length>=4096)throw Error('Слишком много локальных посещений.');index.places.push(id);await save(index);}await store.set(key,value);if(value===null&&index.places.includes(id)){index.places=index.places.filter(p=>p!==id);await save(index);}}),
  clear:(legacyPlaces:readonly number[]=[])=>serialize(indexKey,async()=>{const index=await decode();for(const id of legacyPlaces){if(!Number.isSafeInteger(id)||id<1)throw Error('Некорректное место.');if(!index.places.includes(id))index.places.push(id);}index.deleted=true;await save(index);for(const id of index.places)await store.set(prefix+'.'+id,null);index.places=[];await save(index);}),
 };
}
