import test from 'node:test';
import assert from 'node:assert/strict';
import {createApiClient,decodeProgress} from '../src/api/client.ts';
import {progressFraction,progressPercent} from '../src/progress/presentation.ts';
const empty={total_xp:0,level:1,level_start_xp:0,next_level_xp:100,xp_to_next_level:100};
test('progress uses private GET with bearer, never a client owner/XP parameter',async()=>{
 let call;const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{call={url,init};return Response.json(empty);}});
 assert.deepEqual(await api.getProgress('A'.repeat(43)),empty);assert.equal(call.url,'http://api.test/v1/me/progress');assert.equal(call.init.headers.Authorization,'Bearer '+'A'.repeat(43));assert.equal(call.init.body,undefined);
});
test('progress validates integer/level/null boundaries and consistency',()=>{
 for(const invalid of [{...empty,total_xp:-1},{...empty,level:11},{...empty,level_start_xp:100},{...empty,xp_to_next_level:99},{...empty,next_level_xp:null},{...empty,total_xp:NaN},{...empty,total_xp:9007199254740992},{...empty,level:10}])assert.throws(()=>decodeProgress(invalid),e=>e.kind==='invalid_response');
 const max={total_xp:5000,level:10,level_start_xp:4500,next_level_xp:null,xp_to_next_level:null};assert.deepEqual(decodeProgress(max),max);assert.equal(progressFraction(max),1);
 assert.equal(progressFraction(empty),0);assert.equal(progressFraction({total_xp:200,level:2,level_start_xp:100,next_level_xp:300,xp_to_next_level:100}),.5);
});
test('offline and unauthorized progress never become fake zero balance',async()=>{
 for(const status of [401,500]){const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>Response.json({error:{code:'unavailable',message:'Недоступно'}},{status})});await assert.rejects(api.getProgress('A'.repeat(43)),e=>e.status===status);}
 const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>{throw TypeError('offline');}});await assert.rejects(api.getProgress('A'.repeat(43)),e=>e.kind==='network');
});

test('progress bar never claims completion just below the next threshold',()=>{
 assert.equal(progressPercent({total_xp:299,level:2,level_start_xp:100,next_level_xp:300,xp_to_next_level:1}),99);
 assert.equal(progressPercent(empty),0);
 assert.equal(progressPercent({total_xp:4500,level:10,level_start_xp:4500,next_level_xp:null,xp_to_next_level:null}),100);
});
