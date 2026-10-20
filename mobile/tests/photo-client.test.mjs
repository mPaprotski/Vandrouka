import test from 'node:test';
import assert from 'node:assert/strict';
import {createApiClient,decodeUploadedPhoto,ApiError} from '../src/api/client.ts';
const photo={uri:'file:///cache/test.jpg',size:1000,width:1600,height:1200};
const response={id:'00000000-0000-4000-8000-000000000001',place_id:7,status:'ready',mime_type:'image/jpeg',size_bytes:999,width:1600,height:1200,created_at:'2026-10-26T12:00:00Z',ready_at:'2026-10-26T12:00:01Z'};
const token='A'.repeat(43);
test('native multipart has only place/file, bearer header and generated boundary; deletion accepts 204',async()=>{
 const NativeFormData=globalThis.FormData;
 class Form{entries=[];append(...entry){this.entries.push(entry);}}
 globalThis.FormData=Form;
 try{
  const calls=[];const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{calls.push({url,init});return init.method==='DELETE'?new Response(null,{status:204}):Response.json(response);}});
  assert.deepEqual(await api.uploadPhoto(7,photo,token),response);
  const {url,init}=calls[0];assert.equal(url,'http://api.test/v1/visits/photo-upload');assert.equal(init.method,'POST');assert.equal(init.headers.Authorization,'Bearer '+token);assert.equal(init.headers['Content-Type'],undefined);
  assert.deepEqual(init.body.entries,[['place_id','7'],['file',{uri:photo.uri,type:'image/jpeg',name:'visit.jpg'}]]);
  assert.equal(await api.deletePhoto(response.id,token),undefined);assert.equal(calls[1].init.method,'DELETE');
  assert.ok(!JSON.stringify(init.body.entries).includes('latitude'));assert.ok(!url.includes(token));
 }finally{globalThis.FormData=NativeFormData;}
});
test('malformed/foreign-place/oversized response is rejected',async()=>{
 for(const bad of [{...response,id:'bad'},{...response,status:'uploading'},{...response,size_bytes:10485761},{...response,width:9000},{...response,ready_at:null}])assert.throws(()=>decodeUploadedPhoto(bad),e=>e.kind==='invalid_response');
 const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>Response.json({...response,place_id:8})});
 await assert.rejects(api.uploadPhoto(7,photo,token),e=>e.kind==='invalid_response');
});
test('file validation, backend errors, timeout and abort are actionable without exposing file paths',async()=>{
 let calls=0;const api=createApiClient({baseUrl:'http://api.test',fetcher:async()=>{calls++;return Response.json({error:{code:'rate_limited',message:'Повторите через минуту'}},{status:429,headers:{'X-Request-ID':'photo-request'}});}});
 for(const bad of [{...photo,uri:'https://elsewhere/test.jpg'},{...photo,size:10485761},{...photo,width:9000}])assert.throws(()=>api.uploadPhoto(7,bad,token),e=>e.kind==='invalid_request');
 assert.equal(calls,0);
 await assert.rejects(api.uploadPhoto(7,photo,token),e=>e instanceof ApiError&&e.status===429&&e.requestId==='photo-request');
 const pending=createApiClient({baseUrl:'http://api.test',fetcher:()=>new Promise(()=>{})});
 await assert.rejects(pending.uploadPhoto(7,photo,token,{timeoutMs:10}),e=>e.kind==='timeout');
 const abort=new AbortController();const work=pending.uploadPhoto(7,photo,token,{signal:abort.signal});abort.abort();await assert.rejects(work,e=>e.kind==='cancelled');
});

test('automatic draft discard requests the server guard against deleting confirmed media',async()=>{
 let url;const api=createApiClient({baseUrl:'http://api.test',fetcher:async(value)=>{url=value;return Response.json({error:{code:'photo_confirmed',message:'Фото подтверждено'}},{status:409});}});
 await assert.rejects(api.discardPhoto(response.id,token),e=>e.status===409&&e.code==='photo_confirmed');
 assert.equal(url,`http://api.test/v1/visits/photos/${response.id}?unconfirmed_only=true`);
});
