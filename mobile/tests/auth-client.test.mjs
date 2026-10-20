import assert from 'node:assert/strict';
import test from 'node:test';
import {createApiClient,ApiError} from '../src/api/client.ts';
const user={id:1,email:'user@example.com',created_at:'2026-10-22T12:00:00Z'};
const session={user,access_token:'A'.repeat(43),refresh_token:'b'.repeat(43),token_type:'Bearer',expires_at:'2026-10-23T12:00:00Z',refresh_expires_at:'2026-11-21T12:00:00Z'};
test('auth posts JSON secrets only in body, me uses bearer, logout handles 204',async()=>{
 const calls=[];const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{
  calls.push({url,init});
  if(url.endsWith('/logout'))return new Response(null,{status:204});
  return new Response(JSON.stringify(url.endsWith('/me')?{user}:session));
 }});
 const input={email:' user@example.com ',password:' password with spaces ',user_id:99};
 assert.deepEqual(await api.register(input),session);assert.deepEqual(await api.login(input),session);
 assert.deepEqual(await api.refresh(session.refresh_token),session);assert.deepEqual(await api.me(session.access_token),user);
 assert.equal(await api.logout(session.refresh_token),undefined);
 assert.deepEqual(JSON.parse(calls[0].init.body),{email:'user@example.com',password:input.password});
 assert.equal(calls[0].init.method,'POST');assert.equal(calls[0].init.headers['Content-Type'],'application/json');
 assert.deepEqual(JSON.parse(calls[2].init.body),{refresh_token:session.refresh_token});
 assert.equal(calls[3].init.headers.Authorization,'Bearer '+session.access_token);
 for(const call of calls){assert.ok(!call.url.includes(session.access_token));assert.ok(!call.url.includes(session.refresh_token));}
});
test('malformed token/date/session payload is rejected',async()=>{
 for(const value of [{...session,refresh_token:'wrong'},{...session,token_type:'Cookie'},{...session,expires_at:'2026-10-23T12:00:00'},{...session,refresh_expires_at:'2026-10-22T12:00:00Z'},{...session,user:{...user,id:0}}]){
  const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>new Response(JSON.stringify(value))});
  await assert.rejects(api.login({email:'user@example.com',password:'valid password 123'}),error=>error instanceof ApiError&&error.kind==='invalid_response');
 }
});
