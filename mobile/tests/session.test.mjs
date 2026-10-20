import assert from 'node:assert/strict';
import test from 'node:test';
import {ApiError} from '../src/api/client.ts';
import {createSessionController} from '../src/auth/session.ts';
const now=Date.parse('2026-10-22T12:00:00Z');
function pair(id=1,mark='A',expires=now+86400000){return {user:{id,email:`user${id}@example.com`,created_at:new Date(now).toISOString()},access_token:mark.repeat(43),refresh_token:mark.toLowerCase().repeat(43),token_type:'Bearer',expires_at:new Date(expires).toISOString(),refresh_expires_at:new Date(now+30*86400000).toISOString()};}
function fixture(session=null){
 let raw=session?JSON.stringify({version:1,origin:'http://api.test',session,pending:[]}):null;
 const calls={refresh:0,me:0,logout:[]};
 const storage={read:async()=>raw,write:async value=>{raw=value;}};
 const api={login:async()=>pair(),register:async()=>pair(),refresh:async()=>{calls.refresh++;return pair(1,'B');},logout:async token=>{calls.logout.push(token);},me:async()=>{calls.me++;return pair().user;}};
 const create=()=>createSessionController({api,storage,origin:'http://api.test',now:()=>now});
 return {api,storage,calls,create,record:()=>JSON.parse(raw),setRaw:value=>{raw=value;}};
}
const credentials={email:'user@example.com',password:'test password 123'};
const unauthorized=()=>new ApiError('http','Сессия завершилась.',{status:401});
const offline=()=>new ApiError('network','Нет связи с сервером.');
function deferred(){let resolve;const promise=new Promise(r=>{resolve=r;});return {promise,resolve};}

test('restores and verifies stored session; never stores passwords',async()=>{
 const f=fixture(pair()),controller=f.create();await controller.resume();
 assert.equal(controller.getSnapshot().status,'authenticated');assert.equal(f.calls.me,1);
 assert.equal(f.record().session.user.id,1);
 await controller.logout();assert.equal(controller.getSnapshot().status,'guest');assert.equal(f.record().session,null);
 await controller.signIn('register',credentials);
 assert.equal(controller.getSnapshot().status,'authenticated');assert.ok(!JSON.stringify(f.record()).includes(credentials.password));
});
test('concurrent expired requests rotate once and persist new tokens',async()=>{
 const f=fixture(pair(1,'A',now-1));const controller=f.create();
 await controller.resume();assert.equal(f.calls.refresh,1);assert.equal(f.record().session.access_token,'B'.repeat(43));
 const fresh=f.create();await fresh.resume();assert.equal(f.calls.refresh,1);
 const slow=deferred(),g=fixture(pair(1,'A',now-1));g.api.refresh=async()=>{g.calls.refresh++;await slow.promise;return pair(1,'B');};
 const c=g.create(),boot=c.resume();await new Promise(resolve=>setImmediate(resolve));
 const one=c.withAccess(async token=>token),two=c.withAccess(async token=>token);
 slow.resolve();await boot;assert.deepEqual(await Promise.all([one,two]),['B'.repeat(43),'B'.repeat(43)]);assert.equal(g.calls.refresh,1);
});
test('revoked or fully expired sessions become guest without false success',async()=>{
 const f=fixture(pair());f.api.me=async()=>{throw unauthorized();};f.api.refresh=async()=>{throw unauthorized();};
 const controller=f.create();await controller.resume();assert.equal(controller.getSnapshot().status,'guest');assert.equal(f.record().session,null);
 const expiredPair=pair();expiredPair.expires_at=new Date(now-2).toISOString();expiredPair.refresh_expires_at=new Date(now-1).toISOString();
 const g=fixture(expiredPair);await g.create().resume();assert.equal(g.record().session,null);assert.equal(g.calls.refresh,0);
});
test('offline restore blocks private UI but preserves retry credentials',async()=>{
 const f=fixture(pair());f.api.me=async()=>{throw offline();};const c=f.create();await c.resume();
 assert.equal(c.getSnapshot().status,'error');assert.equal(f.record().session.refresh_token,'a'.repeat(43));
 f.api.me=async()=>pair().user;await c.resume();assert.equal(c.getSnapshot().status,'authenticated');
});
test('offline logout survives restart and never restores that account',async()=>{
 const f=fixture(pair()),c=f.create();await c.resume();f.api.logout=async()=>{throw offline();};await c.logout();
 assert.equal(c.getSnapshot().status,'guest');assert.equal(f.record().session,null);assert.deepEqual(f.record().pending,['a'.repeat(43)]);
 const restarted=f.create();await restarted.resume();assert.equal(restarted.getSnapshot().status,'guest');assert.equal(f.calls.me,1);
 f.api.logout=async token=>f.calls.logout.push(token);await restarted.resume();assert.equal(f.record().pending.length,0);assert.equal(restarted.getSnapshot().status,'guest');
});
test('late private result is discarded across logout/account switch',async()=>{
 const f=fixture(pair()),c=f.create();await c.resume();const delayed=deferred();
 const old=c.withAccess(async()=>delayed.promise).catch(error=>error);
 await c.logout();f.api.login=async()=>pair(2,'C');await c.signIn('login',credentials);delayed.resolve({private:'old account'});
 const result=await old;assert.equal(result.kind,'cancelled');assert.equal(c.getSnapshot().user.id,2);assert.equal(f.record().session.user.id,2);
});
test('logout during refresh cannot resurrect the old account',async()=>{
 const f=fixture(pair(1,'A',now-1)),delayed=deferred();f.api.refresh=async()=>delayed.promise;
 const c=f.create(),boot=c.resume();await new Promise(resolve=>setImmediate(resolve));
 await c.logout();delayed.resolve(pair(1,'B'));await boot;
 assert.equal(c.getSnapshot().status,'guest');assert.equal(f.record().session,null);assert.ok(f.calls.logout.includes('b'.repeat(43)));
});
test('native storage failure never publishes authenticated state and revokes issued session',async()=>{
 const f=fixture(),c=f.create();await c.resume();f.storage.write=async()=>{throw new Error('private native details');};
 await assert.rejects(c.signIn('login',credentials),/защищённое хранилище/);
 assert.equal(c.getSnapshot().status,'error');assert.ok(f.calls.logout.includes('a'.repeat(43)));assert.ok(!c.getSnapshot().message.includes('private'));
});
test('tokens bound to another API origin are discarded without network use',async()=>{
 const f=fixture(pair());f.setRaw(JSON.stringify({version:1,origin:'http://different.test',session:pair(),pending:[]}));const c=f.create();await c.resume();
 assert.equal(c.getSnapshot().status,'guest');assert.equal(f.calls.me,0);assert.equal(f.calls.logout.length,0);assert.equal(f.record().session,null);
});

test('an old screen cannot execute with a newly signed-in owner session',async()=>{
 const f=fixture(pair(2,'C'));f.api.me=async()=>pair(2,'C').user;const c=f.create();await c.resume();let calls=0;
 await assert.rejects(c.withAccess(async()=>{calls++;return 'wrong owner';},1),e=>e.kind==='cancelled');assert.equal(calls,0);
 assert.equal(await c.withAccess(async()=> 'owner two',2),'owner two');
});
