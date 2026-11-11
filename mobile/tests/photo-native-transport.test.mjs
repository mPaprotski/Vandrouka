import test from 'node:test';
import assert from 'node:assert/strict';
import {registerHooks} from 'node:module';

// Load the actual native adapter and API client, with only the device File module mocked.
const fileModule='data:text/javascript,'+encodeURIComponent(`export class File extends globalThis.File {constructor(uri){super(['jpeg-fixture'],'visit.jpg',{type:'image/jpeg'});this.uri=uri;}}`);
const hooks=registerHooks({resolve(specifier,context,next){
 if(specifier==='expo-file-system')return {url:fileModule,shortCircuit:true};
 return next(specifier,context);
}});
const {createApiClient}=await import('../src/api/client.ts');
const {photoFile}=await import('../src/api/photoFile.native.ts');
hooks.deregister();

test('native upload supplies a real File to standards-based multipart instead of a URI descriptor',async()=>{
 const result={id:'00000000-0000-4000-8000-000000000001',place_id:7,status:'ready',mime_type:'image/jpeg',size_bytes:999,width:1600,height:1200,created_at:'2026-10-26T12:00:00Z',ready_at:'2026-10-26T12:00:01Z'};
 const api=createApiClient({baseUrl:'http://api.test',fetcher:async(url,init)=>{
  assert.equal(url,'http://api.test/v1/visits/photo-upload');assert.equal(init.headers['Content-Type'],undefined);
  const part=init.body.get('file');assert.ok(part instanceof File);assert.equal(part.type,'image/jpeg');
  const wire=await new Response(init.body).formData();assert.equal(wire.get('place_id'),'7');assert.equal(await wire.get('file').text(),'jpeg-fixture');
  assert.equal(wire.get('file').name,'visit.jpg');return Response.json(result);
 }});
 assert.deepEqual(await api.uploadPhoto(7,{uri:'file:///cache/Camera/test.jpg',width:1600,height:1200,size:1000,file:photoFile('file:///cache/Camera/test.jpg')},'A'.repeat(43)),result);
});
