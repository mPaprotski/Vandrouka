import {ApiError} from '../api/client.ts';
import type {HistoryPage} from '../api/client.ts';
export type VisitFilter='all'|'visited'|'unvisited';
/** Publish only a complete result: a partial page must never imply "unvisited". */
export async function loadVisitedPlaces(load:(cursor:string|undefined,signal:AbortSignal)=>Promise<HistoryPage>,signal:AbortSignal):Promise<ReadonlySet<number>>{
 const ids=new Set<number>(),cursors=new Set<string>();let cursor:string|undefined;
 do{
  if(signal.aborted)throw new ApiError('cancelled','Загрузка карты отменена.');
  const page=await load(cursor,signal);
  if(signal.aborted)throw new ApiError('cancelled','Загрузка карты отменена.');
  for(const visit of page.visits)ids.add(visit.place_id);
  if(page.next_cursor===null)return ids;
  if(cursors.has(page.next_cursor))throw new ApiError('invalid_response','Не удалось загрузить все посещения. Обновите карту.');
  cursors.add(page.next_cursor);cursor=page.next_cursor;
 }while(true);
}
export function filterVisited<T extends {id:number}>(places:readonly T[],ids:ReadonlySet<number>|null,filter:VisitFilter):T[]{
 if(filter!=='all'&&ids===null)return [];
 return places.filter(place=>filter==='all'||(filter==='visited'?ids!.has(place.id):!ids!.has(place.id)));
}
