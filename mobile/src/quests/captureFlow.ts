import * as Crypto from 'expo-crypto';
import { ApiError } from '../api/errors';
import { api } from '../api/client';
import type { LocalPhoto } from '../api/client';
import { locateForPhoto, removeLocalPhoto } from '../visits/native';
import { visitFixReady } from '../visits/photo';
import type { questDraftStorage, QuestDraft } from './draftStorage';
import type { QuestScope } from './requests';
import type { Attempt, Submission } from './types';

type Storage = ReturnType<typeof questDraftStorage>;
const gpsErrors = new Set(['too_far', 'poor_accuracy', 'stale_location', 'future_location']);
export type CaptureData = { record?: QuestDraft; photo?: LocalPhoto; attempt?: Attempt };
type Save = (data: CaptureData) => void;

export function captureFlow(scope: QuestScope, participation: string, storage: Storage) {
  async function prepareImage(task: string, attempt: string | undefined, take: () => Promise<LocalPhoto>, signal: AbortSignal): Promise<CaptureData> {
    const raw = await take();
    try {
      if (signal.aborted) return {};
      const photo = await storage.stage(raw);
      try {
        if (signal.aborted) return {};
        const file = await storage.photo(photo);
        if (!file) throw new Error('Снимок не сохранился. Сделайте другой.');
        const record = { task, attempt, photo };
        await storage.write(record);
        return { record, photo: file };
      } catch (error) {
        await storage.forgetPhoto(photo).catch(() => {});
        throw error;
      } finally {
        if (signal.aborted) {
          // Preserve an already indexed file. Otherwise remove the temporary camera copy.
          try {
            const indexed = await storage.read();
            if (indexed?.photo?.name !== photo.name) await storage.forgetPhoto(photo);
          } catch { /* Keep a possibly indexed photo for recovery. */ }
        }
      }
    } finally {
      await removeLocalPhoto(raw.uri).catch(() => {});
    }
  }

  async function upload(data: CaptureData, signal: AbortSignal, update: Save): Promise<CaptureData> {
    const { record, photo } = data;
    if (!record?.photo || !photo) throw new Error('Черновик снимка недоступен. Сделайте новый снимок.');
    const existing = record.attempt;
    let attempt = await scope.withAccess(auth => existing ? api.quests.attempt(existing, auth) :
      api.quests.createAttempt(participation, record.task, auth), signal);
    if (attempt.task_id !== record.task || attempt.participation_id !== participation) throw new Error('Снимок относится к другому заданию.');
    const saved = { ...record, attempt: attempt.id };
    await storage.write(saved);
    if (!signal.aborted) update({ record: saved, photo, attempt });
    if (signal.aborted) return { record: saved, photo, attempt };
    if (attempt.status !== 'draft') return resolveSent(attempt);
    if (!attempt.photo_ready) {
      if (attempt.uploading) throw new ApiError('http', 'Снимок ещё обрабатывается.', {
        status: 409, code: 'photo_upload_in_progress', retryAfterMs: 2000,
      });
      attempt = await scope.withAccess(auth => api.quests.upload(attempt.id, photo, auth), signal);
    }
    if (attempt.status !== 'draft') return resolveSent(attempt);
    if (!attempt.photo_ready) throw new Error('Обработанный снимок пока не готов.');
    const ready = { task: record.task, attempt: attempt.id };
    await storage.write(ready);
    return { record: ready, attempt };
  }

  async function resolveSent(attempt: Attempt): Promise<CaptureData> {
    await storage.write(undefined);
    return { attempt };
  }

  async function submit(attempt: Attempt, previous: QuestDraft | undefined, signal: AbortSignal, update: Save): Promise<CaptureData> {
    // Reconcile with the server before regenerating GPS or repeating an earlier request.
    const latest = await scope.withAccess(auth => api.quests.attempt(attempt.id, auth), signal);
    if (latest.status !== 'draft') return resolveSent(latest);
    if (!latest.photo_ready) throw new ApiError('http', 'Снимок больше недоступен. Сделайте новый.', { status: 409, code: 'photo_not_ready' });
    let submission: Submission | undefined = previous?.attempt === attempt.id ? previous.submission : undefined;
    if (!submission) {
      const fix = await locateForPhoto(signal);
      if (!visitFixReady(fix) || fix.accuracy === null) throw new Error('Нужен свежий GPS с точностью до 30 м.');
      submission = { key: Crypto.randomUUID(), location: {
        lat: fix.latitude, lon: fix.longitude, accuracy_m: fix.accuracy, observed_at: new Date(fix.timestamp).toISOString(),
      } };
    }
    const record = { task: attempt.task_id, attempt: attempt.id, submission };
    await storage.write(record);
    if (signal.aborted) return { record, attempt: latest };
    update({ record, attempt: latest });
    try {
      const result = await scope.withAccess(auth => api.quests.submit(attempt.id, submission, auth), signal);
      return resolveSent(result);
    } catch (error) {
      if (error instanceof ApiError && error.status === 422 && error.code && gpsErrors.has(error.code)) {
        // A definite GPS rejection leaves the image uploaded and permits a new fix.
        const fresh = { task: record.task, attempt: attempt.id };
        await storage.write(fresh);
        if (!signal.aborted) update({ record: fresh, attempt: latest });
      }
      throw error;
    }
  }

  async function retake(data: CaptureData, attempt: Attempt | undefined, signal: AbortSignal): Promise<CaptureData> {
    let withdrawn: Attempt | undefined;
    if (data.record?.submission) {
      const id = data.record.attempt;
      if (!id) throw new Error('Попытка ещё не проверена.');
      const current = await scope.withAccess(auth => api.quests.attempt(id, auth), signal);
      if (current.status !== 'draft') return resolveSent(current);
      if (current.photo_ready) throw new Error('Сначала проверьте результат отправки.');
      // The server confirms no submitted work and no usable photo.
      withdrawn = await scope.withAccess(auth => api.quests.withdraw(current.id, auth), signal);
    } else if (attempt?.status === 'draft' || data.record?.attempt) {
      const id = data.record?.attempt ?? attempt?.id;
      if (id) {
        const current = await scope.withAccess(auth => api.quests.attempt(id, auth), signal);
        if (current.status === 'accepted' || current.status === 'pending_review' || current.status === 'rejected') return resolveSent(current);
        if (current.status === 'draft') withdrawn = await scope.withAccess(auth => api.quests.withdraw(id, auth), signal);
      }
    }
    await storage.write(undefined);
    return { attempt: withdrawn };
  }

  return { prepareImage, upload, submit, retake };
}
