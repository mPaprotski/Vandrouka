import test from 'node:test';
import assert from 'node:assert/strict';
import {createApiClient} from '../src/api/client.ts';
test('delete operations use authenticated DELETE and no caller-selected owner',async()=>{
 const calls=[];const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{calls.push({url,init});return new Response(null,{status:204});}});
 await api.deleteAccount('current password','A'.repeat(43));await api.hideVisit(42,'A'.repeat(43));
 assert.equal(calls[0].url,'http://api.test/v1/me');assert.equal(calls[0].init.method,'DELETE');assert.deepEqual(JSON.parse(calls[0].init.body),{password:'current password'});assert.equal(calls[0].init.headers.Authorization,'Bearer '+'A'.repeat(43));
 assert.equal(calls[1].url,'http://api.test/v1/users/me/visits/42');assert.equal(calls[1].init.method,'DELETE');assert.equal(calls[1].init.body,undefined);
 for(const id of [0,-1,NaN,1.5])assert.throws(()=>api.hideVisit(id,'A'.repeat(43)));
});
test('failed deletion never becomes false success',async()=>{
 for(const status of [403,404,429,500]){
  const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>Response.json({error:{code:'denied',message:'Ошибка удаления'}},{status})});await assert.rejects(api.deleteAccount('current password','A'.repeat(43)),e=>e.status===status);
 }
});
