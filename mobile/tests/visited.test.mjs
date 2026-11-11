import test from 'node:test';
import assert from 'node:assert/strict';
import {loadVisitedPlaces,filterVisited} from '../src/exploration/visited.ts';
const page=(ids,next=null)=>({visits:ids.map(place_id=>({place_id})),next_cursor:next});
test('personal map loads every page, deduplicates repeats and does not mutate catalog',async()=>{
 const calls=[];const signal=new AbortController().signal;
 const ids=await loadVisitedPlaces(async(cursor,s)=>{assert.equal(s,signal);calls.push(cursor);return cursor===undefined?page([9,9],'a'):cursor==='a'?page([2],'b'):page([9,7]);},signal);
 assert.deepEqual(calls,[undefined,'a','b']);assert.deepEqual([...ids],[9,2,7]);
 const catalog=[{id:1},{id:2},{id:7}];assert.deepEqual(filterVisited(catalog,ids,'visited'),[{id:2},{id:7}]);assert.deepEqual(filterVisited(catalog,ids,'unvisited'),[{id:1}]);assert.deepEqual(filterVisited(catalog,ids,'all'),catalog);assert.deepEqual(catalog,[{id:1},{id:2},{id:7}]);
});
test('unknown and failed history never imply unvisited',async()=>{
 for(const filter of ['visited','unvisited'])assert.deepEqual(filterVisited([{id:1}],null,filter),[]);
 assert.deepEqual(filterVisited([{id:1}],null,'all'),[{id:1}]);
 await assert.rejects(loadVisitedPlaces(async cursor=>{if(cursor)throw Error('offline');return page([1],'a');},new AbortController().signal),/offline/);
 assert.deepEqual([...await loadVisitedPlaces(async()=>page([]),new AbortController().signal)],[]);
});
test('cancelled or looping histories never publish a partial personal map',async()=>{
 const abort=new AbortController();let resolve,calls=0;
 const pending=loadVisitedPlaces(async()=>{calls++;return new Promise(r=>resolve=r);},abort.signal);abort.abort();resolve(page([1],'a'));await assert.rejects(pending,e=>e.kind==='cancelled');assert.equal(calls,1);
 await assert.rejects(loadVisitedPlaces(async()=>page([1],'a'),new AbortController().signal),e=>e.kind==='invalid_response');
});
test('fresh load after confirmation or account change derives state from that account only',async()=>{
 const signal=new AbortController().signal;
 assert.deepEqual([...await loadVisitedPlaces(async()=>page([]),signal)],[]);
 assert.deepEqual([...await loadVisitedPlaces(async()=>page([10]),signal)],[10]);
 assert.deepEqual([...await loadVisitedPlaces(async()=>page([20]),signal)],[20]);
});
