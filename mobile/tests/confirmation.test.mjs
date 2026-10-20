import assert from 'node:assert/strict';
import test from 'node:test';
import {createPhotoController} from '../src/visits/photo.ts';
import {createAttemptStorage,decodeAttempt} from '../src/visits/attempt.ts';
import {ApiError} from '../src/api/client.ts';
const id='00000000-0000-4000-8000-000000000001';
const key='00000000-0000-4000-8000-000000000010';
const photo={uri:'file:///cache/test.jpg',width:1600,height:1200,size:1000};
const uploaded={id,place_id:7,status:'ready',mime_type:'image/jpeg',size_bytes:1000,width:1600,height:1200,created_at:'2026-10-27T12:00:00Z',ready_at:'2026-10-27T12:00:01Z'};
const fix=()=>({latitude:53,longitude:27,accuracy:5,timestamp:Date.now()});
const success=(xp=100)=>({visit:{id:1,place_id:7,photo_id:id,status:'confirmed',created_at:'2026-10-27T12:00:00Z',confirmed_at:'2026-10-27T12:00:00Z'},xp_awarded:xp});
const rejected=()=>({visit:{id:1,place_id:7,photo_id:id,status:'rejected',created_at:'2026-10-27T12:00:00Z',rejection_code:'too_far'},xp_awarded:0,error:{code:'too_far',message:'Подойдите ближе к месту'}});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};};
function setup(overrides={},saved={value:null}){
 const calls={sent:[],locations:0,remote:[],keys:0};
 const storage={read:async()=>structuredClone(saved.value),write:async(value)=>{saved.value=structuredClone(value);}};
 const c=createPhotoController({locate:async()=>{calls.locations++;return fix();},upload:async()=>uploaded,removeRemote:async id=>{calls.remote.push(id);},removeLocal:async()=>{},...overrides,
 confirmation:{storage,key:()=>{calls.keys++;return key.slice(0,-1)+calls.keys;},send:async(k,input)=>{assert.equal(saved.value.key,k);calls.sent.push({key:k,input:structuredClone(input)});return success();},...overrides.confirmation}});
 return {c,calls,saved,storage};
}
async function prepare(c){await c.restore();await c.capture(async()=>photo);await c.upload();assert.equal(c.getSnapshot().status,'uploaded');}
test('fresh GPS for confirmation, +100/0 success, persistent-before-send and media retention',async()=>{
 for(const xp of [100,0]){
  const {c,calls,saved}=setup({confirmation:{send:async()=>success(xp)}});await prepare(c);assert.equal(calls.locations,1);
  await c.confirm();assert.equal(calls.locations,2);assert.equal(c.getSnapshot().status,'confirmed');assert.equal(c.getSnapshot().result.xp_awarded,xp);assert.equal(saved.value,null);
  c.dispose();assert.deepEqual(calls.remote,[]);
 }
});
test('lost response survives restart and retries exact key/body without refreshing GPS',async()=>{
 const store={value:null};const first=setup({confirmation:{send:async()=>{throw new ApiError('timeout','Ответ потерян');}}},store);
 await prepare(first.c);await first.c.confirm();assert.equal(first.c.getSnapshot().status,'uncertain');const attempt=structuredClone(store.value);
 first.c.retake();assert.equal(first.c.getSnapshot().status,'uncertain');first.c.dispose();assert.deepEqual(first.calls.remote,[]);
 const next=setup({},store);await next.c.restore();assert.equal(next.c.getSnapshot().status,'uncertain');await next.c.confirm();
 assert.deepEqual(next.calls.sent,[{key:attempt.key,input:attempt.input}]);assert.equal(next.calls.locations,0);assert.equal(next.calls.keys,0);assert.equal(next.c.getSnapshot().status,'confirmed');next.c.dispose();assert.deepEqual(next.calls.remote,[]);
});
test('stored rejection permits a fresh key/fix using the same photo; no automatic retry',async()=>{
 let sends=0;const {c,calls}=setup({confirmation:{send:async()=>++sends===1?rejected():success(0)}});
 await prepare(c);await c.confirm();assert.equal(c.getSnapshot().status,'rejected');assert.equal(sends,1);
 await c.confirm();assert.equal(c.getSnapshot().status,'confirmed');assert.equal(calls.keys,2);assert.equal(calls.locations,3);c.dispose();assert.deepEqual(calls.remote,[]);
});
test('double tap sends once; background/close never deletes possibly confirmed media',async()=>{
 const work=deferred();let sends=0,signal;const {c,calls,saved}=setup({confirmation:{send:async(_,__,s)=>{sends++;signal=s;return work.promise;}}});
 await prepare(c);const op=c.confirm();await new Promise(r=>setImmediate(r));assert.equal(c.getSnapshot().status,'confirming');await c.confirm();assert.equal(sends,1);
 c.interrupt();assert.equal(signal.aborted,true);assert.equal(c.getSnapshot().status,'uncertain');c.dispose();work.resolve(success());await op;assert.ok(saved.value);assert.deepEqual(calls.remote,[]);
});
test('storage failure prevents send; retry uses original attempt, not a new key',async()=>{
 let fail=true,saved=null,sends=0;const {c,calls}=setup({confirmation:{storage:{read:async()=>null,write:async value=>{if(fail)throw Error('store');saved=value;}},send:async()=>{sends++;return success();}}});
 await prepare(c);await c.confirm();assert.equal(sends,0);assert.equal(c.getSnapshot().status,'uncertain');fail=false;await c.confirm();assert.equal(sends,1);assert.equal(calls.keys,1);assert.equal(calls.locations,2);assert.equal(saved,null);c.dispose();assert.deepEqual(calls.remote,[]);
});
test('GPS failure starts no attempt; server/malformed responses preserve pending photo',async()=>{
 const {c,calls,saved}=setup();await prepare(c);
 const gpsFail=setup({locate:async()=>{throw Error('GPS denied');}});await gpsFail.c.restore();await gpsFail.c.capture(async()=>photo);await gpsFail.c.upload();assert.equal(gpsFail.c.getSnapshot().status,'preview');gpsFail.c.dispose();
 c.dispose();assert.equal(saved.value,null);assert.deepEqual(calls.remote,[id]);
 for(const error of [new ApiError('http','Занято',{status:500}),new ApiError('invalid_response','Неверный ответ')]){
  const a=setup({confirmation:{send:async()=>{throw error;}}});await prepare(a.c);await a.c.confirm();assert.ok(a.saved.value);a.c.dispose();assert.deepEqual(a.calls.remote,[]);
 }
});
test('read failure blocks new uploads rather than overwriting unknown pending data',async()=>{
 const {c}=setup({confirmation:{storage:{read:async()=>{throw Error('locked');},write:async()=>assert.fail('must not overwrite')}}});
 await c.restore();assert.equal(c.getSnapshot().status,'storage_error');await c.capture(async()=>assert.fail('must not capture'));c.dispose();
});
test('journal isolates owner/origin/place and compares before clearing/writing across controllers',async()=>{
 const attempt={key,photo:uploaded,input:{photo_id:id,location:{lat:53,lon:27,accuracy_m:10,observed_at:'2026-10-27T12:00:00Z'}}};
 let raw=null;const port={get:async()=>raw,set:async(_,v)=>{raw=v;}};
 const a=createAttemptStorage('http://api',1,7,Promise.resolve('test-key'),port),b=createAttemptStorage('http://api',1,7,Promise.resolve('test-key'),port);
 await a.write(attempt);assert.deepEqual(await b.read(),attempt);
 for(const [origin,owner,place] of [['http://other',1,7],['http://api',2,7],['http://api',1,8]])await assert.rejects(createAttemptStorage(origin,owner,place,Promise.resolve('test-key'),port).read());
 await assert.rejects(b.write({...attempt,key:key.slice(0,-1)+'2'}));await assert.rejects(b.write(null,'wrong-key'));assert.ok(raw);
 await b.write(null,key);assert.equal(raw,null);
 const next={...attempt,key:key.slice(0,-1)+'2'};await b.write(next);await assert.rejects(a.write(null,key));assert.equal((await b.read()).key,next.key);
 assert.throws(()=>decodeAttempt({...attempt,input:{...attempt.input,photo_id:'other'}},7));
});

test('a GPS error before confirmation leaves the upload reusable and sends nothing',async()=>{
 let locations=0;const a=setup({locate:async()=>{if(++locations===2)throw Error('GPS denied');return fix();}});
 await prepare(a.c);await a.c.confirm();assert.equal(a.c.getSnapshot().status,'uploaded');assert.equal(a.saved.value,null);assert.equal(a.calls.sent.length,0);a.c.dispose();assert.deepEqual(a.calls.remote,[id]);
});
test('failure to clear journal after a server success preserves photo and replays stored result',async()=>{
 let saved=null,clearFails=true,sends=0;const a=setup({confirmation:{storage:{read:async()=>saved,write:async value=>{if(value===null&&clearFails)throw Error('locked');saved=structuredClone(value);}},send:async()=>{sends++;return success();}}});
 await prepare(a.c);await a.c.confirm();assert.equal(a.c.getSnapshot().status,'uncertain');assert.ok(saved);clearFails=false;await a.c.confirm();assert.equal(a.c.getSnapshot().status,'confirmed');assert.equal(sends,2);assert.equal(a.calls.keys,1);a.c.dispose();assert.deepEqual(a.calls.remote,[]);
});

test('authoritative missing/consumed photo clears attempt for retake but never deletes media',async()=>{
 for(const error of [new ApiError('http','Нет фото',{status:404,code:'not_found'}),new ApiError('http','Фото использовано',{status:409,code:'photo_already_used'})]){
  const a=setup({confirmation:{send:async()=>{throw error;}}});await prepare(a.c);await a.c.confirm();assert.equal(a.c.getSnapshot().status,'photoExpired');assert.equal(a.saved.value,null);a.c.retake();assert.equal(a.c.getSnapshot().status,'camera');a.c.dispose();assert.deepEqual(a.calls.remote,[]);
 }
});

test('cancelling fresh confirmation GPS sends no request and leaves no pending journal',async()=>{
 const gps=deferred();let calls=0;const a=setup({locate:async()=>++calls===1?fix():gps.promise});await prepare(a.c);
 const pending=a.c.confirm();assert.equal(a.c.getSnapshot().status,'confirmLocating');a.c.interrupt();a.c.dispose();gps.resolve(fix());await pending;
 assert.equal(a.saved.value,null);assert.deepEqual(a.calls.remote,[id]);assert.equal(a.calls.sent.length,0);
});
test('competing journal writes cannot overwrite a different request',async()=>{
 let raw=null;const port={get:async()=>raw,set:async(_,v)=>{await Promise.resolve();raw=v;}};
 const make=()=>createAttemptStorage('http://api',1,7,Promise.resolve('race-key'),port);
 const attempt={key,photo:uploaded,input:{photo_id:id,location:{lat:53,lon:27,accuracy_m:10,observed_at:'2026-10-27T12:00:00Z'}}};
 const results=await Promise.allSettled([make().write(attempt),make().write({...attempt,key:key.slice(0,-1)+'3'})]);
 assert.equal(results.filter(r=>r.status==='fulfilled').length,1);assert.equal(results.filter(r=>r.status==='rejected').length,1);assert.equal((await make().read()).key,key);
});
