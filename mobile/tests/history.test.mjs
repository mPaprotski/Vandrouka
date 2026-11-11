import test from 'node:test';
import assert from 'node:assert/strict';
import {createHistoryController} from '../src/history/controller.ts';
import {createApiClient,decodeHistoryPage} from '../src/api/client.ts';
import {privatePhotoDataUri} from '../src/history/photo.ts';
const item=id=>({id,place_id:1,place_name:'Музей',place_active:true,timezone:'Europe/Minsk',created_at:'2026-10-30T12:00:00Z',confirmed_at:'2026-10-30T12:00:00Z',photo_id:null,xp_awarded:100});
const page=(ids,next=null)=>({visits:ids.map(item),next_cursor:next});
test('history pagination retries without losing rows and removes duplicate IDs',async()=>{
 let calls=0;const c=createHistoryController(async(cursor)=>{calls++;if(!cursor)return page([3,2],'abc');if(calls===2)throw Error('offline');return page([2,1]);});
 await c.refresh();await c.more();assert.deepEqual(c.getSnapshot().items.map(v=>v.id),[3,2]);assert.equal(c.getSnapshot().next,'abc');assert.equal(c.getSnapshot().moreError,'offline');
 await c.more();assert.deepEqual(c.getSnapshot().items.map(v=>v.id),[3,2,1]);assert.equal(c.getSnapshot().next,null);
});
test('late page cannot repopulate cleared private history',async()=>{
 let resolve;const c=createHistoryController(()=>new Promise(r=>resolve=r));const pending=c.refresh();c.suspend();resolve(page([1]));await pending;assert.deepEqual(c.getSnapshot().items,[]);assert.equal(c.getSnapshot().status,'idle');
});
test('refresh wins over an older request and looping cursors become retryable errors',async()=>{
 let resolve,calls=0;const c=createHistoryController(async()=>{calls++;if(calls===1)return new Promise(r=>resolve=r);return page([2],'abc');});
 const old=c.refresh();await c.refresh();resolve(page([1]));await old;assert.equal(c.getSnapshot().items[0].id,2);await c.more();assert.match(c.getSnapshot().moreError,/повторил/);
});
test('history decodes empty, private photo absent, invalid and duplicate rows',()=>{
 assert.deepEqual(decodeHistoryPage(page([])),page([]));assert.deepEqual(decodeHistoryPage(page([1])),page([1]));
 for(const bad of [page([1,1]),{...page([]),next_cursor:'abc'},{...page([1]),visits:[{...item(1),xp_awarded:99}]},{...page([1]),visits:[{...item(1),photo_id:'secret-url'}]}])assert.throws(()=>decodeHistoryPage(bad));
});
test('private photos use bearer and no-store; invalid images and foreign access fail',async()=>{
 let call;const client=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{call={url,init};return new Response(new Blob(['jpeg'],{type:'image/jpeg'}),{headers:{'Content-Type':'image/jpeg'}});}});
 const blob=await client.getPrivatePhoto('00000000-0000-4000-8000-000000000001','A'.repeat(43));assert.equal(blob.size,4);assert.equal(call.init.headers.Authorization,'Bearer '+'A'.repeat(43));assert.equal(call.init.cache,'no-store');assert.equal(call.init.redirect,'error');
 for(const response of [new Response('text',{headers:{'Content-Type':'text/html'}}),new Response('',{headers:{'Content-Type':'image/jpeg'}}),Response.json({error:{code:'not_found',message:'Нет фото'}},{status:404})]){
 const client=createApiClient({baseUrl:'http://api.test',fetcher:async()=>response});await assert.rejects(client.getPrivatePhoto('00000000-0000-4000-8000-000000000001','A'.repeat(43)));
 }
});
test('private image conversion supports cancellation without persisting any files',async()=>{
 let reader;const make=()=>reader={result:null,onload:null,onerror:null,onabort:null,readAsDataURL(){},abort(){this.aborted=true;}};
 const abort=new AbortController();const pending=privatePhotoDataUri(new Blob(['x']),abort.signal,make);abort.abort();await assert.rejects(pending,e=>e.kind==='cancelled');assert.equal(reader.aborted,true);assert.equal(reader.onload,null);
 const ready=privatePhotoDataUri(new Blob(['x']),new AbortController().signal,make);reader.result='data:image/jpeg;base64,eA==';reader.onload();assert.equal(await ready,reader.result);
});
