import {photoFile} from '../api/photoFile.native';
import {validateLocalPhoto} from './localPhoto';
import * as FileSystem from 'expo-file-system/legacy';
import * as Location from 'expo-location';
import type { CameraView } from 'expo-camera';
import { locateOnce, LocationError } from '../location/locate';
import { visitFixReady } from './photo';

export async function takeLocalPhoto(camera:CameraView){
 const picture=await camera.takePictureAsync({quality:0.8,exif:false,base64:false,skipProcessing:false});
 try{
  const info=await FileSystem.getInfoAsync(picture.uri);
  return {...validateLocalPhoto(picture,info),file:photoFile(picture.uri)};
 }catch(error){await removeLocalPhoto(picture.uri);throw error;}
}
export async function removeLocalPhoto(uri:string){
 // Only camera-generated files inside this project's cache are ever removed.
 if(FileSystem.cacheDirectory&&uri.startsWith(FileSystem.cacheDirectory))await FileSystem.deleteAsync(uri,{idempotent:true});
}
export function locateForPhoto(signal:AbortSignal){
 return locateOnce({
  hasServicesEnabledAsync:Location.hasServicesEnabledAsync,
  getForegroundPermissionsAsync:Location.getForegroundPermissionsAsync,
  requestForegroundPermissionsAsync:Location.requestForegroundPermissionsAsync,
  watch:(callback,onError)=>Location.watchPositionAsync({accuracy:Location.Accuracy.High,timeInterval:1000,distanceInterval:0},callback,onError),
 },signal,30000,visitFixReady).catch(error=>{
  if(error instanceof LocationError&&error.code==='timeout')throw new LocationError('timeout','Не удалось получить свежий GPS с точностью до 30 м. Попробуйте на открытом месте.');
  throw error;
 });
}
