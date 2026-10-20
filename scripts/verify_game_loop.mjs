/** Local integration only. Run with Node 22 --experimental-strip-types from any directory.
 * Native camera/GPS/SecureStore are replaced; real API, PostgreSQL and MinIO are required.
 * No Mapbox calls. Generated test users/photos are removed in finally.
 */
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import assert from 'node:assert/strict';
import {createApiClient,ApiError} from '../mobile/src/api/client.ts';
import {createSessionController} from '../mobile/src/auth/session.ts';
import {createPhotoController} from '../mobile/src/visits/photo.ts';
import {createAttemptStorage} from '../mobile/src/visits/attempt.ts';
process.chdir(fileURLToPath(new URL('..',import.meta.url)));
const origin=new URL(process.env.TEST_API_URL??'http://127.0.0.1:8080');
assert.ok(['127.0.0.1','localhost','[::1]'].includes(origin.hostname)&&origin.protocol==='http:'&&!origin.username&&!origin.password&&origin.pathname==='/'&&!origin.search&&!origin.hash,'TEST_API_URL must be an HTTP loopback origin');
const base=origin.origin,originalForm=globalThis.FormData,bytes=readFileSync(new URL('../mobile/tests/fixtures/visit.jpg',import.meta.url));
class NativeForm{entries=[];append(...entry){this.entries.push(entry);}}
let drop=false,far=false,deletes=0,authRaw=null;
const accounts=[],flows=[],journals=new Map(),keys=[];
const api=createApiClient({baseUrl:base,fetcher:async(url,init)=>{
 assert.equal(new URL(url).origin,base,'unexpected external request');
 if(init.body instanceof NativeForm){const form=new originalForm();for(const [key,value] of init.body.entries){if(typeof value==='string')form.append(key,value);else form.append(key,new Blob([bytes],{type:value.type}),value.name);}init={...init,body:form};}
 const response=await fetch(url,{...init,redirect:'error'});
 if(drop&&init.method==='POST'&&/\/places\/\d+\/visits$/.test(url)&&response.ok){drop=false;await response.arrayBuffer();throw new TypeError('Injected lost response after server commit');}
 return response;
}});
let auth=createSessionController({api,storage:{read:async()=>authRaw,write:async v=>{authRaw=v;}},origin:base});
function sql(query){return execFileSync('docker',['compose','--env-file','backend/.env','exec','-T','postgres','psql','-U','postgres','-d','vandrouka','-At','-c',query],{encoding:'utf8'}).trim();}
function expectState(c,status){assert.equal(c.getSnapshot().status,status,c.getSnapshot().message);}
async function register(){
 const account={email:`e2e-${crypto.randomUUID()}@example.invalid`,password:'temporary integration password 123',photos:[]};accounts.push(account);
 await auth.signIn('register',account);account.id=auth.getSnapshot().user.id;
 assert.equal(sql(`SELECT id FROM users WHERE email='${account.email}'`),String(account.id),'API must use this local Compose database');return account;
}
function makeFlow(account,place){
 const journalKey=`${base}:${account.id}:${place.id}`;
 const storage=createAttemptStorage(base,account.id,place.id,Promise.resolve(journalKey),{get:async key=>journals.get(key)??null,set:async(key,value)=>{if(value===null)journals.delete(key);else journals.set(key,value);}});
 const c=createPhotoController({
  locate:async()=>({latitude:far?0:place.lat,longitude:place.lon,accuracy:5,timestamp:Date.now()}),
  upload:(photo,signal)=>auth.withAccess(async token=>{const result=await api.uploadPhoto(place.id,photo,token,{signal});account.photos.push(result.id);return result;},account.id),
  removeRemote:async id=>{deletes++;await auth.withAccess(token=>api.discardPhoto(id,token),account.id);},removeLocal:async()=>{},
  confirmation:{storage,key:()=>crypto.randomUUID(),send:(key,input,signal)=>{keys.push(key);return auth.withAccess(token=>api.confirmVisit(place.id,key,input,token,{signal}),account.id);}},
 });flows.push(c);return c;
}
async function upload(c){await c.restore();await c.capture(async()=>({uri:'file:///cache/fixture.jpg',width:16,height:12,size:bytes.length}));await c.upload();expectState(c,'uploaded');}
try{
 await api.health();globalThis.FormData=NativeForm;await auth.resume();
 const first=await register(),place=(await api.listPlaces('minsk'))[0];assert.ok(place);
 const initial=makeFlow(first,place);await upload(initial);drop=true;await initial.confirm();expectState(initial,'uncertain');assert.equal(journals.size,1);initial.dispose();assert.equal(deletes,0);
 // Simulate JS/process restart: new auth controller reads saved session, new photo
 // controller reads persisted journal. Storage ports represent encrypted native storage.
 const restoredAuth=createSessionController({api,storage:{read:async()=>authRaw,write:async v=>{authRaw=v;}},origin:base});await restoredAuth.resume();assert.equal(restoredAuth.getSnapshot().user.id,first.id);auth=restoredAuth;
 const restored=makeFlow(first,place);await restored.restore();expectState(restored,'uncertain');await restored.confirm();expectState(restored,'confirmed');assert.equal(restored.getSnapshot().result.xp_awarded,100);assert.equal(keys[0],keys[1]);assert.equal(journals.size,0);restored.dispose();assert.equal(deletes,0);
 await assert.rejects(auth.withAccess(token=>api.discardPhoto(first.photos[0],token),first.id),e=>e.status===409&&e.code==='photo_confirmed');
 const own=await auth.withAccess(token=>fetch(`${base}/v1/visits/photos/${first.photos[0]}`,{headers:{Authorization:`Bearer ${token}`},redirect:'error'}),first.id);assert.equal(own.status,200);
 const repeat=makeFlow(first,place);await upload(repeat);await repeat.confirm();expectState(repeat,'confirmed');assert.equal(repeat.getSnapshot().result.xp_awarded,0);repeat.dispose();
 const corrected=makeFlow(first,place);await upload(corrected);far=true;await corrected.confirm();expectState(corrected,'rejected');assert.equal(corrected.getSnapshot().result.error.code,'too_far');far=false;const rejectedKey=keys.at(-1);await corrected.confirm();expectState(corrected,'confirmed');assert.equal(corrected.getSnapshot().result.xp_awarded,0);assert.notEqual(keys.at(-1),rejectedKey);corrected.dispose();
 // Force expiration only for our own test account; the API must refresh and retry.
 sql(`UPDATE sessions SET expires_at=created_at+INTERVAL '1 microsecond' WHERE user_id=${first.id} AND revoked_at IS NULL`);
 await auth.withAccess(token=>api.me(token),first.id);
 const pending=makeFlow(first,place);await upload(pending);drop=true;await pending.confirm();expectState(pending,'uncertain');pending.dispose();assert.equal(journals.size,1);
 await auth.logout();const second=await register();let wrongOwnerCalls=0;
 await assert.rejects(auth.withAccess(async()=>{wrongOwnerCalls++;},first.id),e=>e.kind==='cancelled');assert.equal(wrongOwnerCalls,0);
 const foreign=await auth.withAccess(token=>fetch(`${base}/v1/visits/photos/${first.photos[0]}`,{headers:{Authorization:`Bearer ${token}`},redirect:'error'}),second.id);assert.equal(foreign.status,404);
 await assert.rejects(auth.withAccess(token=>api.discardPhoto(first.photos[0],token),second.id),e=>e.status===404);
 const secondFlow=makeFlow(second,place);await secondFlow.restore();expectState(secondFlow,'camera');await secondFlow.capture(async()=>({uri:'file:///cache/fixture.jpg',width:16,height:12,size:bytes.length}));await secondFlow.upload();await secondFlow.confirm();expectState(secondFlow,'confirmed');assert.equal(secondFlow.getSnapshot().result.xp_awarded,100);secondFlow.dispose();assert.equal(journals.size,1);
 await auth.logout();await auth.signIn('login',first);assert.equal(auth.getSnapshot().user.id,first.id);
 const back=makeFlow(first,place);await back.restore();expectState(back,'uncertain');await back.confirm();expectState(back,'confirmed');assert.equal(back.getSnapshot().result.xp_awarded,0);back.dispose();assert.equal(journals.size,0);assert.equal(deletes,0);
 for(const account of accounts)assert.equal(sql(`SELECT count(*)||':'||sum(amount) FROM xp_transactions WHERE user_id=${account.id}`),'1:100');
 assert.equal(sql(`SELECT count(*) FROM visits WHERE user_id=${first.id}`),'5');assert.equal(sql(`SELECT count(*) FROM visits WHERE user_id=${second.id}`),'1');
 console.log('PASS: two accounts; session restart/refresh; lost response and exact replay; +100/0 XP; GPS rejection/correction; owner privacy; guarded discard retains confirmed photos.');
}finally{
 flows.forEach(c=>c.dispose());globalThis.FormData=originalForm;await auth.logout().catch(()=>{});
 let cleanupFailed=false;
 for(const account of accounts){
  try{
   if(account.id){const pair=await api.login(account);for(const id of account.photos)await api.deletePhoto(id,pair.access_token);await api.logout(pair.refresh_token);}
  }catch{cleanupFailed=true;console.error('Test photo cleanup failed; database cascade will queue private object deletion.');}
  try{assert.match(account.email,/^e2e-[a-f0-9-]{36}@example\.invalid$/);sql(`DELETE FROM users WHERE email='${account.email}'`);}catch{cleanupFailed=true;console.error('Test account cleanup failed.');}
 }
 if(cleanupFailed)process.exitCode=1;
}
