import type {LocalPhoto} from '../api/client.ts';
/** Android Camera returns `.jpg`; JS declarations also describe `jpg`. */
export function validateLocalPhoto(picture:{uri:string;width:number;height:number;format?:string},info:{exists:boolean;isDirectory?:boolean;size?:number}):LocalPhoto{
 const format=picture.format?.toLowerCase().replace(/^\./,'');
 const size=info.size;
 if(!['jpg','jpeg'].includes(format??'')||!picture.uri.startsWith('file://')||!info.exists||info.isDirectory||typeof size!=='number'||!Number.isSafeInteger(size)||size<1||size>10*1024*1024||!Number.isSafeInteger(picture.width)||!Number.isSafeInteger(picture.height)||picture.width<1||picture.height<1||picture.width>8192||picture.height>8192||picture.width*picture.height>20000000)throw new Error('invalid photo');
 return {uri:picture.uri,width:picture.width,height:picture.height,size};
}
