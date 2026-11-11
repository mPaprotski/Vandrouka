import type {Progress} from '../api/client.ts';
/** UI ratio uses server-provided bounds; the level table exists only on the server. */
export function progressFraction(p:Progress):number{
 if(p.next_level_xp===null)return 1;
 return Math.max(0,Math.min(1,(p.total_xp-p.level_start_xp)/(p.next_level_xp-p.level_start_xp)));
}

export function progressPercent(p:Progress):number{
 return p.next_level_xp===null?100:Math.min(99,Math.floor(progressFraction(p)*100));
}
