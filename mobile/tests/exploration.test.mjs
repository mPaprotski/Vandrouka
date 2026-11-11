import test from 'node:test';
import assert from 'node:assert/strict';
import {createApiClient,decodeExploration} from '../src/api/client.ts';
const at='2026-11-05T12:00:00Z';
const state=(count=0)=>({discovered_places:count,achievements:[1,5,10].map((target,i)=>({code:['first_discovery','five_discoveries','ten_discoveries'][i],name:'Достижение',target,progress:Math.min(count,target),earned_at:count>=target?at:null})),collections:[{code:'center-v1',name:'Исторический центр',city:'minsk',total:3,visited:0,completed_at:null,places:[1,2,3].map(id=>({slug:'place-'+id,name:'Место',place_id:id,available:true,visited:false}))}]});
test('decode fixed achievement boundaries and incomplete/complete collection',()=>{
 for(const count of [0,1,4,5,9,10,11])assert.deepEqual(decodeExploration(state(count)),state(count));
 const full=state(3);full.collections[0].places.forEach(p=>p.visited=true);full.collections[0].visited=3;full.collections[0].completed_at=at;assert.deepEqual(decodeExploration(full),full);
 const missing=state();missing.collections[0].places[0]={...missing.collections[0].places[0],place_id:null,available:false};assert.deepEqual(decodeExploration(missing),missing);
});
test('reject impossible or duplicate awards and partial/fake completion responses',()=>{
 const cases=[];let bad=state();bad.achievements[0].earned_at=at;cases.push(bad);
 bad=state(5);bad.achievements[1].earned_at=null;cases.push(bad);
 bad=state(5);bad.achievements[1].progress=6;cases.push(bad);
 bad=state();bad.achievements[1].code=bad.achievements[0].code;cases.push(bad);
 bad=state();bad.collections[0].completed_at=at;cases.push(bad);
 bad=state();bad.collections[0].visited=1;cases.push(bad);
 bad=state();bad.collections[0].places[0].place_id=null;cases.push(bad);
 bad=state();bad.collections[0].places[1].slug=bad.collections[0].places[0].slug;cases.push(bad);
 for(const value of cases)assert.throws(()=>decodeExploration(value),e=>e.kind==='invalid_response');
});
test('exploration is authenticated GET without a client owner; offline is an error',async()=>{
 let call;const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{call={url,init};return Response.json(state());}});
 await api.getExploration('A'.repeat(43));assert.equal(call.url,'http://api.test/v1/me/exploration');assert.equal(call.init.headers.Authorization,'Bearer '+'A'.repeat(43));assert.equal(call.init.body,undefined);
 const offline=createApiClient({baseUrl:'http://api.test',fetcher:async()=>{throw TypeError('offline');}});await assert.rejects(offline.getExploration('A'.repeat(43)),e=>e.kind==='network');
});
