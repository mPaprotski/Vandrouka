import test from 'node:test';
import assert from 'node:assert/strict';
import {validateLocalPhoto} from '../src/visits/localPhoto.ts';
const picture={uri:'file:///cache/Camera/test.jpg',width:1600,height:1200,format:'jpg'};
const info={exists:true,isDirectory:false,size:1000};
test('accept actual Android .jpg response and JS jpg format without changing local URI',()=>{
 for(const format of ['.jpg','jpg','.jpeg','jpeg','JPG'])assert.deepEqual(validateLocalPhoto({...picture,format},info),{uri:picture.uri,width:1600,height:1200,size:1000});
});
test('invalid or unsupported files still fail validation',()=>{
 for(const format of ['png','.png','',undefined,'webp'])assert.throws(()=>validateLocalPhoto({...picture,format},info));
 for(const patch of [{uri:'https://example.com/photo.jpg'},{width:0},{height:-1},{width:NaN},{width:8193},{width:5000,height:5000}])assert.throws(()=>validateLocalPhoto({...picture,...patch},info));
 for(const patch of [{exists:false},{isDirectory:true},{size:undefined},{size:NaN},{size:0},{size:10*1024*1024+1}])assert.throws(()=>validateLocalPhoto(picture,{...info,...patch}));
 assert.equal(validateLocalPhoto(picture,{...info,size:10*1024*1024}).size,10*1024*1024);
});
