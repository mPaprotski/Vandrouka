import type { ApiRequest, LocalPhoto, RequestOptions, TransportOptions } from '../api/client.ts';
import { ApiError } from '../api/errors.ts';
import {
  decodeAttempt, decodeEditor, decodeInvite, decodePage, decodeParticipant, decodeParticipation,
  decodeQuest, decodeQuestProgress, decodeReview, decodeRun, isQuestID, matching,
} from './decode.ts';
import type { Draft, Submission } from './types.ts';

export type QuestAccess = RequestOptions & { accessToken: string };

function uuid(value: string) {
  if (!isQuestID(value)) throw new ApiError('invalid_request', 'Некорректная ссылка на квест или задание.');
  return value.toLowerCase();
}
const quest = (id: string) => `/v1/quests/${uuid(id)}`;
const run = (id: string) => `/v1/quest-runs/${uuid(id)}`;
const play = (id: string) => `/v1/quest-participations/${uuid(id)}`;
const attempt = (id: string) => `/v1/quest-attempts/${uuid(id)}`;
const page = (cursor?: string) => cursor ? `?cursor=${uuid(cursor)}` : '';

export function createQuestApi(request: ApiRequest) {
  const send = <T>(path: string, decode: (input: unknown) => T, auth: QuestAccess, options: TransportOptions = {}) =>
    request(path, decode, auth, { ...options, accessToken: auth.accessToken });
  const update = <T>(path: string, decode: (input: unknown) => T, auth: QuestAccess, body?: unknown) =>
    send(path, decode, auth, { method: 'POST', body });

  return {
    templates: (auth: QuestAccess, cursor?: string) => send('/v1/quests/mine' + page(cursor), decodePage(decodeQuest), auth),
    runs: (auth: QuestAccess, cursor?: string) => send('/v1/quest-runs/mine' + page(cursor), decodePage(decodeRun), auth),
    participations: (auth: QuestAccess, cursor?: string) => send('/v1/quest-participations/mine' + page(cursor), decodePage(decodeParticipation), auth),
    create: (cityID: number, auth: QuestAccess) => update('/v1/quests', decodeEditor, auth, { city_id: cityID }),
    editor: (id: string, auth: QuestAccess) => send(quest(id) + '/editor', matching(decodeEditor, uuid(id)), auth),
    save: (id: string, revision: number, draft: Draft, auth: QuestAccess) =>
      send(quest(id), matching(decodeEditor, uuid(id)), auth, { method: 'PUT', body: { ...draft, expected_revision: revision } }),
    publish: (id: string, revision: number, auth: QuestAccess) =>
      update(quest(id) + '/publish', matching(decodeQuest, uuid(id)), auth, { expected_revision: revision }),
    launch: (id: string, revision: number, title: string, auth: QuestAccess) =>
      update(quest(id) + '/runs', decodeInvite, auth, { title, expected_revision: revision }),
    manage: (id: string, auth: QuestAccess) => send(run(id) + '/manage', matching(decodeRun, uuid(id)), auth),
    start: (id: string, auth: QuestAccess) => update(run(id) + '/start', matching(decodeRun, uuid(id)), auth),
    finish: (id: string, auth: QuestAccess) => update(run(id) + '/finish', matching(decodeRun, uuid(id)), auth),
    rotate: (id: string, auth: QuestAccess) => update(run(id) + '/join-code', matching(decodeInvite, uuid(id)), auth),
    joining: (id: string, enabled: boolean, auth: QuestAccess) =>
      send(run(id) + '/joining', matching(decodeRun, uuid(id)), auth, { method: 'PATCH', body: { join_enabled: enabled } }),
    participants: (id: string, auth: QuestAccess, cursor?: string) => send(run(id) + '/participants' + page(cursor), decodePage(decodeParticipant), auth),
    queue: (id: string, auth: QuestAccess, cursor?: string) => send(run(id) + '/reviews' + page(cursor), decodePage(decodeReview), auth),
    review: (id: string, revision: number, decision: 'accept' | 'reject', reason: string, auth: QuestAccess) =>
      update(attempt(id) + '/review', matching(decodeAttempt, uuid(id)), auth,
        { expected_revision: revision, decision, ...(decision === 'reject' ? { reason } : {}) }),
    join: (code: string, name: string, auth: QuestAccess) =>
      update('/v1/quest-participations/join', decodeQuestProgress, auth, { code, display_name: name }),
    progress: (id: string, auth: QuestAccess) => send(play(id), matching(decodeQuestProgress, uuid(id)), auth),
    createAttempt: (participation: string, task: string, auth: QuestAccess) =>
      update(play(participation) + '/attempts', value => {
        const result = decodeAttempt(value);
        if (result.participation_id !== uuid(participation) || result.task_id !== uuid(task)) throw new ApiError('invalid_response', 'Попытка не соответствует заданию.');
        return result;
      }, auth, { task_id: uuid(task) }),
    attempt: (id: string, auth: QuestAccess) => send(attempt(id), matching(decodeAttempt, uuid(id)), auth),
    upload: (id: string, photo: LocalPhoto, auth: QuestAccess) => {
      if (!photo.file || photo.size < 1 || photo.size > 10 * 1024 * 1024 || photo.width * photo.height > 4_000_000) {
        throw new ApiError('invalid_request', 'Снимок недоступен или слишком большой. Сделайте другой.');
      }
      const form = new FormData();
      form.append('file', photo.file);
      return send(attempt(id) + '/photo', matching(decodeAttempt, uuid(id)), { ...auth, timeoutMs: 60000 }, { method: 'PUT', form });
    },
    photo: (id: string, auth: QuestAccess) => send(attempt(id) + '/photo', value => {
      if (!(value instanceof Blob)) throw new ApiError('invalid_response', 'Не удалось прочитать фотографию.');
      return value;
    }, auth, { binary: true }),
    submit: (id: string, data: Submission, auth: QuestAccess) =>
      send(attempt(id) + '/submit', matching(decodeAttempt, uuid(id)), auth,
        { method: 'POST', idempotencyKey: uuid(data.key), body: { location: data.location } }),
    withdraw: (id: string, auth: QuestAccess) => update(attempt(id) + '/withdraw', matching(decodeAttempt, uuid(id)), auth),
  };
}
