import test from 'node:test';
import assert from 'node:assert/strict';
import {createPrivateResource} from '../src/sync/privateResource.ts';
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const deferred=()=>{let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no;});return {promise,resolve,reject};};
test('private requests require foreground and focus; blur removes data and active events cannot fetch hidden screens',async()=>{
 let calls=0;const c=createPrivateResource(async()=>{calls++;return {total_xp:100};});
 await c.refresh();c.setFocused(true);assert.equal(calls,0);c.setActive(true);await tick();assert.equal(c.getSnapshot().data.total_xp,100);
 c.setFocused(false);assert.deepEqual(c.getSnapshot(),{status:'idle'});c.setActive(false);c.setActive(true);await tick();assert.equal(calls,1);
 c.setFocused(true);await tick();assert.equal(calls,2);
});
test('background aborts, clears private data, ignores late completion and refetches on resume',async()=>{
 const late=deferred();let calls=0,signal;
 const c=createPrivateResource(async s=>{signal=s;return ++calls===1?late.promise:{total_xp:200};});c.setActive(true);c.setFocused(true);assert.equal(c.getSnapshot().status,'loading');
 c.setActive(false);assert.equal(signal.aborted,true);assert.deepEqual(c.getSnapshot(),{status:'idle'});
 late.resolve({total_xp:100});await tick();assert.equal(c.getSnapshot().status,'idle');c.setActive(true);await tick();assert.equal(c.getSnapshot().data.total_xp,200);
 c.setActive(true);await tick();assert.equal(calls,2);
});
test('failed refresh clears old success instead of showing cached XP or fake zero',async()=>{
 let offline=false;const c=createPrivateResource(async()=>{if(offline)throw Error('Нет сети');return 100;});c.setActive(true);c.setFocused(true);await tick();assert.equal(c.getSnapshot().data,100);
 offline=true;await c.refresh();assert.deepEqual(c.getSnapshot(),{status:'error',message:'Нет сети',requestId:undefined});
 offline=false;await c.refresh();assert.equal(c.getSnapshot().data,100);
});
test('late retry and old account results cannot replace the current private resource',async()=>{
 const late=deferred();let calls=0;const old=createPrivateResource(async()=>++calls===1?late.promise:'fresh');old.setActive(true);old.setFocused(true);await old.refresh();late.resolve('old');await tick();assert.equal(old.getSnapshot().data,'fresh');
 old.setFocused(false);const next=createPrivateResource(async()=>'second account');next.setActive(true);next.setFocused(true);await tick();assert.equal(next.getSnapshot().data,'second account');assert.equal(old.getSnapshot().status,'idle');
});
