import test from 'node:test';
import assert from 'node:assert/strict';
import {createPhotoController,choosePictureSize,visitFixReady} from '../src/visits/photo.ts';
const photo={uri:'file:///cache/test.jpg',width:1600,height:1200,size:1000};
const uploaded={id:'00000000-0000-4000-8000-000000000001',place_id:7,status:'ready'};
const fix=()=>({latitude:53,longitude:27,accuracy:30,timestamp:Date.now()});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};};
function setup(overrides={}){
 const calls={local:[],remote:[],uploads:0,locates:0};
 const controller=createPhotoController({
  locate:async()=>{calls.locates++;return fix();},
  upload:async()=>{calls.uploads++;return uploaded;},
  removeRemote:async id=>{calls.remote.push(id);},removeLocal:async uri=>{calls.local.push(uri);},...overrides,
 });return {controller,calls};
}
test('capture chooses bounded resolution and GPS accepts only fresh accurate fixes',()=>{
 assert.equal(choosePictureSize(['8000x6000','1920x1080','1600x1200','640x480','bogus','0x10']), '1920x1080');
 assert.equal(choosePictureSize(['5000x4000','garbage']),undefined);
 const now=1000000;const valid={...fix(),timestamp:now-30000};assert.equal(visitFixReady(valid,now),true);
 for(const bad of [{...valid,accuracy:null},{...valid,accuracy:30.00001},{...valid,accuracy:NaN},{...valid,accuracy:-1},{...valid,timestamp:now-30001},{...valid,timestamp:now+5001},{...valid,latitude:91}])assert.equal(visitFixReady(bad,now),false);
});
test('preview -> fresh GPS -> upload is single flight; retake deletes abandoned files',async()=>{
 const work=deferred();const {controller:c,calls}=setup({upload:async()=>{calls.uploads++;return work.promise;}});
 await c.capture(async()=>photo);assert.equal(c.getSnapshot().status,'preview');
 const operation=c.upload();await Promise.resolve();await c.upload();assert.equal(calls.uploads,1);
 work.resolve(uploaded);await operation;assert.equal(c.getSnapshot().status,'uploaded');assert.equal(c.getSnapshot().fix.accuracy,30);
 c.retake();assert.equal(c.getSnapshot().status,'camera');assert.deepEqual(calls.local,[photo.uri]);assert.deepEqual(calls.remote,[uploaded.id]);c.dispose();
});
test('poor/unknown GPS never uploads and retry retains preview',async()=>{
 for(const accuracy of [null,31]){
  const {controller:c,calls}=setup({locate:async()=>({...fix(),accuracy})});
  await c.capture(async()=>photo);await c.upload();assert.equal(calls.uploads,0);assert.equal(c.getSnapshot().status,'preview');assert.match(c.getSnapshot().message,/30 м/);c.dispose();
 }
});
test('permissions/GPS and network failures keep the photo for an explicit retry',async()=>{
 const {controller:c,calls}=setup({locate:async()=>{throw new Error('Доступ к GPS не разрешён');}});
 await c.capture(async()=>photo);await c.upload();assert.equal(c.getSnapshot().status,'preview');assert.match(c.getSnapshot().message,/GPS/);assert.equal(calls.uploads,0);c.dispose();
 const {controller:d}=setup({upload:async()=>{throw new Error('Нет связи');}});
 await d.capture(async()=>photo);await d.upload();assert.equal(d.getSnapshot().photo.uri,photo.uri);assert.equal(d.getSnapshot().status,'preview');assert.equal(d.getSnapshot().uploaded,undefined);d.dispose();
});
test('cancel/background aborts upload; late server response is cleaned and cannot restore success',async()=>{
 const work=deferred();let signal;const {controller:c,calls}=setup({upload:async(_,s)=>{signal=s;return work.promise;}});
 await c.capture(async()=>photo);const operation=c.upload();await Promise.resolve();assert.equal(c.getSnapshot().status,'uploading');
 c.interrupt();assert.equal(signal.aborted,true);assert.equal(c.getSnapshot().status,'preview');
 work.resolve(uploaded);await operation;assert.equal(c.getSnapshot().status,'preview');assert.deepEqual(calls.remote,[uploaded.id]);c.dispose();
});
test('leaving or changing account discards late camera and GPS results',async()=>{
 const camera=deferred();const {controller:c,calls}=setup();const capture=c.capture(()=>camera.promise);c.dispose();camera.resolve(photo);await capture;assert.deepEqual(calls.local,[photo.uri]);
 const gps=deferred();const {controller:d,calls:more}=setup({locate:async()=>gps.promise});await d.capture(async()=>photo);const op=d.upload();d.dispose();gps.resolve(fix());await op;assert.equal(more.uploads,0);assert.deepEqual(more.local,[photo.uri]);
});
