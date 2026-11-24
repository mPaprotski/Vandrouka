import assert from 'node:assert/strict';
import test from 'node:test';
import {createWalkStore,decodeSavedWalk,elapsedWalk} from '../src/walks/session.ts';

const id='00000000-0000-4000-8000-000000000001';
const request={city:'minsk',start:{lat:53.9,lon:27.56},start_at:'2026-11-24T11:00:00Z',budget_minutes:90,pace:'normal',categories:[],include_geometry:true,seed:42};
const stop={place_id:7,name:'Ратуша',location:request.start,arrival:request.start_at,departure:'2026-11-24T11:10:00Z',visit_seconds:600,incoming_leg:{walk_seconds:0,distance_m:0},hours_unconfirmed:false,price_unconfirmed:false};
const variant={variant_id:'v1',profile:'best_match',labels:[],stops:[stop],totals:{walk_seconds:0,visit_seconds:600,total_seconds:600,distance_m:0,cost_minor:0,currency:'BYN',cost_complete:true,stops_count:1,unverified_stops_count:0},explanation:{text:'Маршрут',hours_unconfirmed_count:0,prices_unconfirmed_count:0,category_counts:{}},geometry:null};
const outcome={visit:{id:19,place_id:7,photo_id:id,status:'confirmed',created_at:request.start_at,confirmed_at:request.start_at},xp_awarded:0};
function setup(){
 const slots=[null,null];let fail=false;
 const files={read:async n=>slots[n],write:async(n,data)=>{if(fail){slots[n]='partial';throw Error('disk full');}slots[n]=data;}};
 return {slots,files,setFail:v=>{fail=v;},store:createWalkStore('https://api.test',1,files)};
}
test('walk restores account-bound snapshot, pause and 0-XP progress after restart',async()=>{
 const {store,files,slots}=setup();
 await store.start(id,request,variant,'Europe/Minsk',1000);
 await store.pause(id,3000);
 const restarted=createWalkStore('https://api.test',1,files);
 assert.equal(elapsedWalk(await restarted.read(),9000),2000);
 await restarted.resume(id,9000);
 await restarted.confirm(id,outcome);await restarted.confirm(id,outcome);
 const saved=await restarted.read();assert.deepEqual(saved.visits,{'7':19});
 assert.equal(elapsedWalk(saved,11000),4000);
 assert.throws(()=>decodeSavedWalk(slots[saved.revision%2],'https://api.test',2));
 assert.throws(()=>decodeSavedWalk(slots[saved.revision%2],'https://other.test',1));
 await restarted.finish(id,11000);
 assert.equal(elapsedWalk(await restarted.read(),50000),4000);
 const next='00000000-0000-4000-8000-000000000002';
 await restarted.start(next,request,variant,'Europe/Minsk',12000);
 await restarted.confirm(id,outcome);
 assert.deepEqual((await restarted.read()).visits,{});
});
test('failed new journal write preserves previous record and replay can finish it',async()=>{
 const {store,files,setFail}=setup();
 await store.start(id,request,variant,'Europe/Minsk',1000);
 await assert.rejects(store.start(id,request,variant,'Europe/Minsk',1000));
 setFail(true);await assert.rejects(store.confirm(id,outcome));
 const restarted=createWalkStore('https://api.test',1,files);
 assert.deepEqual((await restarted.read()).visits,{});
 setFail(false);await restarted.confirm(id,outcome);assert.deepEqual((await restarted.read()).visits,{'7':19});
 await restarted.close();await assert.rejects(restarted.confirm(id,outcome));
});
