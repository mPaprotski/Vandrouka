import { ApiError } from '../api/errors.ts';
import type { Attempt, Editor, Invite, Page, Participant, Participation, Quest, QuestProgress, Review, Run, Story, Task } from './types.ts';

function invalid(): never {
  throw new ApiError('invalid_response', 'Сервер вернул данные квеста в неожиданном формате.');
}
export function isQuestID(value: unknown): value is string {
  return typeof value === 'string' && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(value) &&
    value !== '00000000-0000-0000-0000-000000000000';
}
function id(value: unknown) { return isQuestID(value) ? value.toLowerCase() : invalid(); }
function object(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : invalid();
}
function text(value: unknown) { return typeof value === 'string' ? value : invalid(); }
function integer(value: unknown, min = 0) {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= min ? value : invalid();
}
function number(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : invalid();
}
function bool(value: unknown) { return typeof value === 'boolean' ? value : invalid(); }
function date(value: unknown) {
  return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? value : invalid();
}
function optional<T>(value: unknown, parse: (input: unknown) => T) { return value === undefined ? undefined : parse(value); }
function array<T>(value: unknown, parse: (input: unknown) => T, max = 200): T[] {
  return Array.isArray(value) && value.length <= max ? value.map(parse) : invalid();
}
function choice<const S extends readonly string[]>(raw: unknown, ...values: S): S[number] {
  return typeof raw === 'string' && values.includes(raw) ? raw as S[number] : invalid();
}
function xp(value: unknown) { return value === 0 || value === 100 ? value : invalid(); }
export function matching<T extends { id: string }>(parse: (value: unknown) => T, expected: string) {
  return (value: unknown) => {
    const decoded = parse(value);
    return decoded.id === expected ? decoded : invalid();
  };
}
export function decodePage<T>(parse: (item: unknown) => T) {
  return (raw: unknown): Page<T> => {
    const p = object(raw);
    return { items: array(p.items, parse, 30), next_cursor: optional(p.next_cursor, id) };
  };
}
export function decodeQuest(raw: unknown): Quest {
  const q = object(raw);
  return { id: id(q.id), city_id: integer(q.city_id, 1), title: text(q.title), description: text(q.description),
    status: choice(q.status, 'draft', 'published', 'archived'), revision: integer(q.revision, 1) };
}
export function decodeEditor(raw: unknown): Editor {
  return { ...decodeQuest(raw), steps: array(object(raw).steps, value => {
    const s = object(value);
    return { place_id: integer(s.place_id, 1), clue: text(s.clue), photo_task: text(s.photo_task), reveal_text: text(s.reveal_text) };
  }, 20) };
}
export function decodeRun(raw: unknown): Run {
  const r = object(raw);
  return { id: id(r.id), quest_id: id(r.quest_id), quest_revision: integer(r.quest_revision, 1), title: text(r.title),
    status: choice(r.status, 'waiting', 'active', 'finished'), finished_at: optional(r.finished_at, date), join_enabled: bool(r.join_enabled), total_steps: integer(r.total_steps, 2),
    started_at: r.started_at === null ? null : date(r.started_at) };
}
export function decodeInvite(raw: unknown): Invite {
  const join_code = text(object(raw).join_code);
  if (!/^[A-HJ-NP-Z2-9]{8}$/.test(join_code)) return invalid();
  return { ...decodeRun(raw), join_code };
}
export function decodeParticipation(raw: unknown): Participation {
  const p = object(raw);
  const result: Participation = { id: id(p.id), run_id: id(p.run_id), title: text(p.title),
    status: choice(p.status, 'waiting', 'active', 'completed', 'closed'), completed_steps: integer(p.completed_steps),
    total_steps: integer(p.total_steps, 2), joined_at: date(p.joined_at) };
  return result.completed_steps <= result.total_steps && result.total_steps <= 20 ? result : invalid();
}
function decodeTask(raw: unknown): Task {
  const t = object(raw);
  return { task_id: id(t.task_id), position: integer(t.position, 1), clue: text(t.clue), photo_task: text(t.photo_task) };
}
export function decodeAttempt(raw: unknown): Attempt {
  const a = object(raw);
  return { id: id(a.id), participation_id: id(a.participation_id), task_id: id(a.task_id), position: integer(a.position, 1),
    status: choice(a.status, 'draft', 'pending_review', 'rejected', 'accepted', 'withdrawn', 'expired'),
    revision: integer(a.revision, 1), photo_ready: bool(a.photo_ready), uploading: bool(a.uploading),
    submitted_at: optional(a.submitted_at, date), reviewed_at: optional(a.reviewed_at, date),
    rejection_reason: optional(a.rejection_reason, text), xp_awarded: xp(a.xp_awarded) };
}
function decodeStory(raw: unknown): Story {
  const s = object(raw);
  return { task_id: id(s.task_id), position: integer(s.position, 1), place_id: integer(s.place_id, 1),
    place_name: text(s.place_name), address: text(s.address), text: text(s.text), attempt_id: id(s.attempt_id),
    xp_awarded: xp(s.xp_awarded), links: array(s.links, value => {
      const link = object(value);
      return { kind: text(link.kind), title: text(link.title), url: text(link.url) };
    }) };
}
export function decodeQuestProgress(raw: unknown): QuestProgress {
  const p = object(raw);
  const result: QuestProgress = { ...decodeParticipation(raw), current_task: optional(p.current_task, decodeTask),
    current_attempt: optional(p.current_attempt, decodeAttempt), stories: array(p.stories, decodeStory, 20),
    xp_awarded: integer(p.xp_awarded), completed_at: optional(p.completed_at, date), started_at: optional(p.started_at, date), review_pending: optional(p.review_pending, bool) };
  const task = result.current_task;
  const attempt = result.current_attempt;
  if ((result.status === 'active') !== Boolean(task) || (result.status === 'completed') !== Boolean(result.completed_at) ||
      result.stories.length !== result.completed_steps || result.stories.some((s, i) => s.position !== i + 1) ||
      result.xp_awarded !== result.stories.reduce((sum, s) => sum + s.xp_awarded, 0) ||
      (task && task.position !== result.completed_steps + 1) ||
      (attempt && (!task || attempt.participation_id !== result.id || attempt.task_id !== task.task_id || attempt.position !== task.position))) return invalid();
  return result;
}
export function decodeParticipant(raw: unknown): Participant {
  const p = object(raw);
  return { id: id(p.id), display_name: text(p.display_name), completed_steps: integer(p.completed_steps), joined_at: date(p.joined_at) };
}
export function decodeReview(raw: unknown): Review {
  const item = object(raw);
  return { ...decodeAttempt(raw), participant_name: text(item.participant_name), place_name: text(item.place_name),
    photo_task: text(item.photo_task), distance_m: number(item.distance_m), accuracy_m: number(item.accuracy_m) };
}
