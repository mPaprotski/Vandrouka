import {ApiError} from '../api/client.ts';
/** Convert only an authenticated, bounded image into memory. No disk files or token URLs. */
export function privatePhotoDataUri(blob:Blob,signal:AbortSignal,makeReader:()=>FileReader=()=>new FileReader()):Promise<string>{
 return new Promise((resolve,reject)=>{
  if(signal.aborted){reject(new ApiError('cancelled','Загрузка фото отменена.'));return;}
  const reader=makeReader();let finished=false;
  const finish=(error?:Error,value?:string)=>{if(finished)return;finished=true;signal.removeEventListener('abort',cancel);reader.onload=null;reader.onerror=null;reader.onabort=null;if(error)reject(error);else resolve(value!);};
  const cancel=()=>{finish(new ApiError('cancelled','Загрузка фото отменена.'));reader.abort();};
  signal.addEventListener('abort',cancel);
  reader.onload=()=>{const uri=reader.result;if(typeof uri!=='string'||!/^data:image\/(?:jpeg|png);base64,[A-Za-z0-9+/]+={0,2}$/.test(uri))finish(new ApiError('invalid_response','Некорректная фотография.'));else finish(undefined,uri);};
  reader.onerror=()=>finish(new ApiError('invalid_response','Не удалось прочитать фотографию.'));
  reader.onabort=()=>finish(new ApiError('cancelled','Загрузка фото отменена.'));
  try{reader.readAsDataURL(blob);}catch{finish(new ApiError('invalid_response','Не удалось прочитать фотографию.'));}
 });
}
export function historyDate(value:string,timezone:string):string{
 try{return new Intl.DateTimeFormat('ru-RU',{dateStyle:'long',timeStyle:'short',timeZone:timezone}).format(new Date(value));}
 catch{return new Intl.DateTimeFormat('ru-RU',{dateStyle:'long',timeStyle:'short',timeZone:'UTC'}).format(new Date(value))+' UTC';}
}
