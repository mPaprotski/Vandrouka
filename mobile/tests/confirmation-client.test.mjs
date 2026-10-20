import assert from 'node:assert/strict';
import test from 'node:test';
import {createApiClient,decodeVisitOutcome} from '../src/api/client.ts';
const photo='00000000-0000-4000-8000-000000000001',key='00000000-0000-4000-8000-000000000002',token='A'.repeat(43);
const input={photo_id:photo,location:{lat:53,lon:27,accuracy_m:5,observed_at:'2026-10-27T12:00:00Z'}};
const visit={id:1,place_id:7,photo_id:photo,status:'confirmed',created_at:'2026-10-27T12:00:00Z',confirmed_at:'2026-10-27T12:00:00Z'};
const success={visit,xp_awarded:100};
const rejected={visit:{id:2,place_id:7,photo_id:photo,status:'rejected',created_at:visit.created_at,rejection_code:'too_far'},xp_awarded:0,error:{code:'too_far',message:'Подойдите ближе к месту'}};
test('confirmed/new and replay and stored rejection preserve exact request/header and decode outcome',async()=>{
 for(const [status,outcome] of [[201,success],[200,success],[201,{...success,xp_awarded:0}],[422,rejected]]){
  let call;const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{call={url,init};return Response.json(outcome,{status});}});
  assert.deepEqual(await api.confirmVisit(7,key,{...input,user_id:999,xp_awarded:100},token),outcome);
  assert.equal(call.url,'http://api.test/v1/places/7/visits');assert.equal(call.init.headers['Idempotency-Key'],key);assert.equal(call.init.headers.Authorization,'Bearer '+token);assert.equal(call.init.headers['Content-Type'],'application/json');
  assert.deepEqual(JSON.parse(call.init.body),input);assert.ok(!call.url.includes(key));
 }
});
test('reject inconsistent XP/status/HTTP/photo/place rather than reporting success',async()=>{
 for(const invalid of [{...success,xp_awarded:200},{...rejected,xp_awarded:100},{...rejected,error:{code:'stale_location',message:'bad'}},{...success,visit:{...visit,confirmed_at:null}},{...success,visit:{...visit,rejection_code:'too_far'}}])assert.throws(()=>decodeVisitOutcome(invalid));
 for(const [status,body] of [[422,success],[200,rejected],[201,{...success,visit:{...visit,place_id:8}}],[201,{...success,visit:{...visit,photo_id:key}}]]){
  const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>Response.json(body,{status})});
  await assert.rejects(api.confirmVisit(7,key,input,token),e=>e.kind==='invalid_response');
 }
});
test('invalid key never sends; conflicts/timeouts/401 stay errors with request ID for resolution',async()=>{
 let calls=0;
 const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>{calls++;return Response.json({error:{code:'idempotency_conflict',message:'Ключ уже использован'}},{status:409,headers:{'X-Request-ID':'confirm-request'}});}});
 assert.throws(()=>api.confirmVisit(7,'bad',input,token),e=>e.kind==='invalid_request');assert.equal(calls,0);
 await assert.rejects(api.confirmVisit(7,key,input,token),e=>e.status===409&&e.code==='idempotency_conflict'&&e.requestId==='confirm-request');
});
