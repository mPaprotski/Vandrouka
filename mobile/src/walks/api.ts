import type { ApiRequest, RequestOptions } from '../api/client.ts';
import { decodeRecalculatedWalk, decodeWalkResult } from './decode.ts';
import type { WalkInput } from './types.ts';
export type { WalkInput, WalkResult } from './types.ts';
export function createWalkApi(request:ApiRequest) {
  return {
    generate:(input:WalkInput,options:RequestOptions={})=>request('/v1/routes/generate',decodeWalkResult,{timeoutMs:190000,...options},{method:'POST',body:input}),
    recalculate:(input:{request:WalkInput;base_place_ids:number[];change:{type:'add'|'remove';place_id:number}},options:RequestOptions={})=>request('/v1/routes/recalculate',decodeRecalculatedWalk,{timeoutMs:190000,...options},{method:'POST',body:input}),
  };
}
