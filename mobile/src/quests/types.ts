import type { SourceLink, VisitInput } from '../api/client.ts';

export type Quest = {
  id: string; city_id: number; title: string; description: string;
  status: 'draft' | 'published' | 'archived'; revision: number;
};
export type QuestStep = { place_id: number; clue: string; photo_task: string; reveal_text: string };
export type Editor = Quest & { steps: QuestStep[] };
export type Draft = Pick<Editor, 'title' | 'description' | 'steps'>;
export type Run = {
  id: string; quest_id: string; quest_revision: number; title: string;
  status: 'waiting' | 'active' | 'finished'; finished_at?: string; join_enabled: boolean; total_steps: number; started_at: string | null;
};
export type Invite = Run & { join_code: string };
export type Participation = {
  id: string; run_id: string; title: string; status: 'waiting' | 'active' | 'completed' | 'closed';
  completed_steps: number; total_steps: number; joined_at: string;
};
export type Task = { task_id: string; position: number; clue: string; photo_task: string };
export type Attempt = {
  id: string; participation_id: string; task_id: string; position: number;
  status: 'draft' | 'pending_review' | 'accepted' | 'rejected' | 'withdrawn' | 'expired';
  revision: number; photo_ready: boolean; uploading: boolean; rejection_reason?: string;
  submitted_at?: string; reviewed_at?: string; xp_awarded: 0 | 100;
};
export type Story = {
  task_id: string; position: number; place_id: number; place_name: string;
  address: string; text: string; links: SourceLink[]; attempt_id: string; xp_awarded: 0 | 100;
};
export type QuestProgress = Participation & {
  stories: Story[]; current_task?: Task; current_attempt?: Attempt;
  xp_awarded: number; completed_at?: string; started_at?: string; review_pending?: boolean;
};
export type Participant = { id: string; display_name: string; completed_steps: number; joined_at: string };
export type Review = Attempt & {
  participant_name: string; place_name: string; photo_task: string; distance_m: number; accuracy_m: number;
};
export type Page<T> = { items: T[]; next_cursor?: string };
export type Fix = VisitInput['location'];
export type Submission = { key: string; location: Fix };
