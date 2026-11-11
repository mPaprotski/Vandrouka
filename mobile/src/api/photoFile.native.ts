import {File} from 'expo-file-system';
/** SDK 58's default expo/fetch requires a File/Blob, not an RN URI descriptor. */
export function photoFile(uri:string):Blob{
 return new File(uri);
}
