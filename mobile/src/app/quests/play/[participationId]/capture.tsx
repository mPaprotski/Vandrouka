import { CameraView, useCameraPermissions } from 'expo-camera';
import { router, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, AppState, Image, Linking, Platform, Text } from 'react-native';
import { api, ApiError } from '../../../../api/client';
import { QuestPhoto } from '../../../../components/quests/QuestPhoto';
import { QuestError, QuestGate, QuestLoad, QuestScreen, questStyles } from '../../../../components/quests/QuestUI';
import { AppButton } from '../../../../components/ui/AppButton';
import { LoadingState } from '../../../../components/ui/ContentState';
import { getApiBaseUrl } from '../../../../config/api';
import { captureFlow } from '../../../../quests/captureFlow';
import type { CaptureData } from '../../../../quests/captureFlow';
import { isQuestID } from '../../../../quests/decode';
import { questDraftStorage } from '../../../../quests/draftStorage';
import { useQuestRead } from '../../../../quests/requests';
import type { QuestScope } from '../../../../quests/requests';
import { takeLocalPhoto } from '../../../../visits/native';
import { choosePictureSize } from '../../../../visits/photo';

type Work = 'capture' | 'upload' | 'submit' | 'retake';
const working: Record<Work, string> = {
  capture: 'Сохраняем снимок…', upload: 'Загружаем и обрабатываем снимок…',
  submit: 'Проверяем GPS и отправляем снимок…', retake: 'Подготавливаем новый снимок…',
};

function Capture({ scope, id }: { scope: QuestScope; id: string }) {
  const store = useMemo(() => questDraftStorage(getApiBaseUrl(), scope.owner, id), [scope.owner, id]);
  const flow = useMemo(() => captureFlow(scope, id, store), [scope, id, store]);
  const load = useCallback((signal: AbortSignal) => scope.withAccess(auth => api.quests.progress(id, auth), signal), [scope, id]);
  const progress = useQuestRead(load);
  const [local, setLocal] = useState<CaptureData>({});
  const [restoring, setRestoring] = useState(true);
  const [storageError, setStorageError] = useState<unknown>();
  const [error, setError] = useState<unknown>();
  const [active, setActive] = useState(false);
  const [work, setWork] = useState<Work>();
  const [uploadRetryAt, setUploadRetryAt] = useState(0);
  useEffect(() => {
    if (!uploadRetryAt) return;
    const timer = setTimeout(() => setUploadRetryAt(0), Math.max(0, uploadRetryAt - Date.now()));
    return () => clearTimeout(timer);
  }, [uploadRetryAt]);
  const [permission, requestPermission] = useCameraPermissions();
  const [size, setSize] = useState<string>();
  const [cameraError, setCameraError] = useState<string>();
  const [preview, setPreview] = useState<string>();
  const camera = useRef<CameraView>(null);
  const cameraVersion = useRef(0);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const screenSignal = useRef<AbortSignal | undefined>(undefined);

  const restore = useCallback(async (signal: AbortSignal) => {
    setRestoring(true);
    setStorageError(undefined);
    try {
      const record = await store.read();
      const photo = record?.photo ? await store.photo(record.photo) : undefined;
      if (!signal.aborted) setLocal({ record, photo });
    } catch (cause) {
      if (!signal.aborted) setStorageError(cause);
    } finally {
      if (!signal.aborted) setRestoring(false);
    }
  }, [store]);

  useFocusEffect(useCallback(() => {
    const controller = new AbortController();
    screenSignal.current = controller.signal;
    function visible(value: boolean) {
      setActive(value);
      if (!value) { cameraVersion.current++; inFlight.current?.abort(); setSize(undefined); setPreview(undefined); }
      else if (!inFlight.current) void restore(controller.signal);
    }
    visible(AppState.currentState === 'active');
    const listener = AppState.addEventListener('change', state => visible(state === 'active'));
    return () => { controller.abort(); listener.remove(); visible(false); };
  }, [restore]));

  const task = progress.data?.current_task;
  const fromServer = progress.data?.current_attempt;
  const known = local.attempt;
  const record = local.record;
  const matchingLocal = !task || !record || record.task === task.task_id;
  const newerLocal = known && known.task_id === task?.task_id &&
    (!fromServer || (known.id === fromServer.id && known.revision >= fromServer.revision) ||
      (known.id === record?.attempt && fromServer.status === 'rejected'));
  const shownAttempt = newerLocal ? known : fromServer;
  const attempt = shownAttempt?.status === 'withdrawn' ? undefined : shownAttempt;
  const latest = attempt ?? (known?.task_id === record?.task ? known : undefined);
  const ready = latest?.status === 'draft' && latest.photo_ready;
  const sent = progress.data?.status === 'completed' || progress.data?.status === 'closed' || (attempt && ['pending_review', 'accepted'].includes(attempt.status));
  const showCamera = active && !restoring && storageError == null && matchingLocal && !!task && !ready && !record?.submission &&
    !record?.photo && !sent && !latest?.uploading && permission?.granted;

  function goBack() {
    router.dismissTo({ pathname: '/quests/play/[participationId]', params: { participationId: id } });
  }

  async function run(kind: Work, action: (signal: AbortSignal, update: (next: CaptureData) => void) => Promise<CaptureData>) {
    if (inFlight.current || !active) return;
    const controller = new AbortController();
    inFlight.current = controller;
    setWork(kind);
    setError(undefined);
    const update = (next: CaptureData) => { if (!controller.signal.aborted) setLocal(next); };
    try {
      const result = await action(controller.signal, update);
      if (controller.signal.aborted) return;
      update(result);
      if (kind === 'retake') { setPreview(undefined); setCameraError(undefined); }
      if (result.attempt?.status && ['pending_review', 'accepted', 'rejected', 'expired'].includes(result.attempt.status)) goBack();
      else if (kind !== 'capture') progress.refresh();
    } catch (cause) {
      if (!controller.signal.aborted) {
        if (kind === 'upload' && cause instanceof ApiError && cause.retryAfterMs) {
          setUploadRetryAt(Date.now() + cause.retryAfterMs);
        }
        setError(cause);
        progress.refresh();
      }
    } finally {
      inFlight.current = undefined;
      setWork(undefined);
      if (controller.signal.aborted && screenSignal.current && !screenSignal.current.aborted && AppState.currentState === 'active') {
        void restore(screenSignal.current);
      }
    }
  }

  async function cameraReady() {
    const version = ++cameraVersion.current;
    setCameraError(undefined);
    try {
      const available = await camera.current?.getAvailablePictureSizesAsync();
      if (version !== cameraVersion.current) return;
      const selected = choosePictureSize(available ?? []);
      if (!selected) throw new Error('Камера не предложила подходящий размер снимка. Откройте экран снова.');
      setSize(selected);
    } catch (cause) {
      if (version === cameraVersion.current) setCameraError(cause instanceof Error ? cause.message : 'Камера недоступна.');
    }
  }

  function retake() {
    Alert.alert('Сделать новый снимок?', 'Текущий черновик снимка будет удалён.', [
      { text: 'Отмена', style: 'cancel' }, { text: 'Переснять', onPress: () => void run('retake', signal =>
        flow.retake(local, latest?.status === 'draft' ? latest : undefined, signal)) },
    ]);
  }
  function recoverSubmission() {
    if (latest?.id === record?.attempt && record?.submission && latest) {
      void run('submit', (signal, update) => flow.submit(latest, record, signal, update));
      return;
    }
    if (record?.attempt) void run('submit', async signal => {
      const current = await scope.withAccess(auth => api.quests.attempt(record.attempt!, auth), signal);
      return flow.submit(current, record, signal, next => { if (!signal.aborted) setLocal(next); });
    });
  }

  return <QuestScreen title={task ? `Фото · этап ${task.position}` : 'Фото задания'}
    description={task?.photo_task}>
    {!progress.data && !local.photo && !record?.submission && <QuestLoad error={progress.error} retry={progress.refresh} />}
    {restoring && <LoadingState message="Ищем сохранённый снимок…" />}
    <QuestError error={storageError ?? error ?? (record && progress.error)} />
    {storageError != null && <AppButton title="Повторить восстановление" variant="secondary"
      onPress={() => screenSignal.current && void restore(screenSignal.current)} />}
    {uploadRetryAt > 0 && <Text style={questStyles.note}>Снимок сохранён. Следующая попытка станет доступна автоматически.</Text>}
    {work && <LoadingState message={working[work]} />}
    {(!matchingLocal || sent || (progress.data && !task)) && <Text style={questStyles.note}>Состояние задания изменилось. Вернитесь к прохождению.</Text>}

    {matchingLocal && !sent && !restoring && storageError == null && <>
      {task && !permission && !ready && !record?.photo && <LoadingState message="Проверяем доступ к камере…" />}
      {latest?.uploading && !ready && <>
        <Text style={questStyles.note}>Снимок обрабатывается на сервере.</Text>
        <AppButton title="Проверить снимок" variant="secondary" disabled={!!work} onPress={progress.refresh} />
      </>}
      {showCamera && <CameraView ref={camera} style={questStyles.photo} pictureSize={size} facing="back" mode="picture"
        onCameraReady={() => void cameraReady()} onMountError={() => { setSize(undefined); setCameraError('Камера занята или недоступна.'); }} />}
      {cameraError && <Text accessibilityRole="alert" style={questStyles.error}>{cameraError}</Text>}
      {task && !ready && !record?.photo && !record?.submission && permission && !permission.granted &&
        <AppButton title={permission.canAskAgain ? 'Разрешить камеру' : 'Открыть настройки камеры'} onPress={() => {
          const ask = permission.canAskAgain ? requestPermission() : Linking.openSettings();
          void ask.catch(cause => setError(cause));
        }} />}
      {showCamera && task && <AppButton title="Сделать снимок" disabled={!size || !!work}
        onPress={() => void run('capture', signal => flow.prepareImage(task.task_id,
          attempt?.status === 'draft' ? attempt.id : undefined, async () => {
            if (!camera.current) throw new Error('Камера недоступна.');
            return takeLocalPhoto(camera.current);
          }, signal))} />}

      {local.photo && !ready && <Image source={{ uri: local.photo.uri }} style={questStyles.photo}
        accessibilityLabel="Предпросмотр исходного снимка" resizeMode="contain" resizeMethod="resize" />}
      {record?.photo && !local.photo && !ready && <Text style={questStyles.note}>Телефон очистил временный снимок. Сделайте новый или попробуйте восстановить ранее загруженное фото.</Text>}
      {!ready && local.photo && <AppButton title={uploadRetryAt ? 'Повтор пока недоступен' : record?.attempt ? 'Проверить и продолжить загрузку' : 'Загрузить фото'}
        disabled={!!work || uploadRetryAt > 0} onPress={() => void run('upload', (signal, update) => flow.upload(local, signal, update))} />}
      {!ready && record?.photo && !local.photo && <AppButton title="Проверить сервер" variant="secondary" disabled={!!work}
        onPress={() => progress.refresh()} />}

      {ready && latest && <QuestPhoto key={latest.id} id={latest.id} scope={scope} onReady={value => setPreview(value ? latest.id : undefined)} />}
      {ready && latest && !record?.submission && <AppButton title="Отправить на проверку" disabled={preview !== latest.id || !!work}
        onPress={() => void run('submit', (signal, update) => flow.submit(latest, record, signal, update))} />}
      {record?.submission && <>
        <Text style={questStyles.note}>Сохранилась незавершённая отправка. Проверьте её результат после подключения.</Text>
        <AppButton title="Проверить результат отправки" disabled={!!work} onPress={recoverSubmission} />
      </>}
      {(record?.photo || ready || latest?.uploading || (record?.submission && !ready)) && <AppButton title="Переснять" variant="secondary" disabled={!!work} onPress={retake} />}
      {ready && error != null && <AppButton title="Открыть настройки телефона" variant="secondary" disabled={!!work}
        onPress={() => { void Linking.openSettings().catch(cause => setError(cause)); }} />}
    </>}
    <AppButton title="К заданию" variant="secondary" onPress={goBack} />
  </QuestScreen>;
}

export default function QuestCaptureScreen() {
  const { participationId } = useLocalSearchParams<{ participationId: string }>();
  return <QuestGate title="Фото задания">{scope =>
    Platform.OS === 'web' || !isQuestID(participationId) ?
      <QuestScreen title="Фото задания" description={Platform.OS === 'web' ? 'Откройте квест на телефоне.' : 'Некорректная ссылка на задание.'} /> :
      <Capture scope={scope} id={participationId.toLowerCase()} />
  }</QuestGate>;
}
