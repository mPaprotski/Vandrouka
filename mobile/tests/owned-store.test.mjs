import test from 'node:test';
import assert from 'node:assert/strict';
import {createOwnedStore} from '../src/visits/ownedStore.ts';
const setup=()=>{const data=new Map();const store={get:async k=>data.get(k)??null,set:async(k,v)=>{if(v===null)data.delete(k);else data.set(k,v);}};return {data,store};};
test('owner deletion clears all indexed places, preserves another owner and blocks late writes',async()=>{
 const {data,store}=setup();const one=createOwnedStore('origin.1',store),two=createOwnedStore('origin.2',store);
 await Promise.all([one.set('origin.1.7','private GPS A'),one.set('origin.1.8','private GPS B'),two.set('origin.2.7','other GPS')]);
 assert.deepEqual(JSON.parse(data.get('origin.1.index')).places,[7,8]);await one.clear();
 assert.equal(data.has('origin.1.7'),false);assert.equal(data.has('origin.1.8'),false);assert.equal(await two.get('origin.2.7'),'other GPS');
 await assert.rejects(one.set('origin.1.9','late GPS'),/удалён/);await assert.rejects(one.get('origin.1.7'),/удалён/);
 await one.clear();assert.equal(JSON.parse(data.get('origin.1.index')).deleted,true);
});
test('legacy journals are indexed when read and explicit legacy candidates are erased',async()=>{
 const {data,store}=setup();data.set('p.7','old journal');data.set('p.8','old unvisited journal');const owned=createOwnedStore('p',store);
 assert.equal(await owned.get('p.7'),'old journal');await owned.clear([8]);assert.equal(data.has('p.7'),false);assert.equal(data.has('p.8'),false);
});
test('failed index write prevents an untracked journal; failed deletion remains retryable and blocks writes',async()=>{
 const {data,store}=setup();let failIndex=true,failRemove=false;
 const guarded={...store,set:async(k,v)=>{if((failIndex&&k==='p.index')||(failRemove&&v===null))throw Error('storage failure');await store.set(k,v);}};
 const owned=createOwnedStore('p',guarded);await assert.rejects(owned.set('p.1','GPS'));assert.equal(data.has('p.1'),false);
 failIndex=false;await owned.set('p.1','GPS');failRemove=true;await assert.rejects(owned.clear());assert.equal(JSON.parse(data.get('p.index')).deleted,true);
 await assert.rejects(owned.set('p.2','late'));failRemove=false;await owned.clear();assert.equal(data.has('p.1'),false);
});
test('untrusted or cross-owner keys cannot read or erase other records',async()=>{
 const {data,store}=setup();const owned=createOwnedStore('p.1',store);
 for(const key of ['p.2.7','p.1.index','p.1.0','p.1.01','p.1.-1'])await assert.rejects(owned.set(key,'x'));
 data.set('p.1.index','{"version":1,"deleted":false,"places":["other"]}');await assert.rejects(owned.clear());
});
