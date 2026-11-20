import assert from 'node:assert/strict';
import test from 'node:test';
import { createBrowserProbe } from '../src/auth/browser-probe.ts';
const id='a'.repeat(43),secret='b'.repeat(64);
const origin='https://probe.test';
const valid={id,browser_url:`${origin}/oauth/begin/${id}`,expires_at:'2026-12-01T12:00:00Z'};

test('probe sends proof in JSON and only opens its exact server URL',async()=>{
 const calls=[];
 const probe=createBrowserProbe(origin,async(url,init)=>{calls.push({url,init});return Response.json(url.endsWith('/attempts')?valid:{status:'success'});});
 assert.deepEqual(await probe.start('google','c'.repeat(64)),valid);
 assert.equal(await probe.result(id,secret),'success');
 assert.deepEqual(JSON.parse(calls[1].init.body),{id,secret});
 assert.ok(!calls[1].url.includes(secret));
 for(const browser_url of ['https://attacker.test/',`${origin}/oauth/begin/${id}?secret=leak`,'javascript:alert(1)']){
  await assert.rejects(createBrowserProbe(origin,async()=>Response.json({...valid,browser_url})).start('google','challenge'));
 }
});
test('pending is not successful login and errors remain explicit',async()=>{
 assert.equal(await createBrowserProbe(origin,async()=>Response.json({status:'pending'})).result(id,secret),'pending');
 await assert.rejects(createBrowserProbe(origin,async()=>Response.json({status:'authenticated'})).result(id,secret));
 await assert.rejects(createBrowserProbe(origin,async()=>new Response('',{status:503})).start('yandex','challenge'),/не настроен/);
 await assert.rejects(createBrowserProbe(origin,async()=>new Response('',{status:401})).result(id,secret),/истёк/);
});
