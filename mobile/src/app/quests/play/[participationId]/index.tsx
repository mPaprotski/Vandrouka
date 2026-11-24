import { router, useFocusEffect, useLocalSearchParams } from 'expo-router';
import { useCallback, useState } from 'react';
import { Alert, Platform, Text } from 'react-native';
import { api } from '../../../../api/client';
import { QuestStory } from '../../../../components/quests/QuestStory';
import { QuestError, QuestGate, QuestLoad, QuestPanel, QuestScreen, questStyles } from '../../../../components/quests/QuestUI';
import { AppButton } from '../../../../components/ui/AppButton';
import { getApiBaseUrl } from '../../../../config/api';
import { historyDate } from '../../../../history/photo';
import { questDraftStorage } from '../../../../quests/draftStorage';
import { useQuestAction, useQuestRead } from '../../../../quests/requests';
import type { QuestScope } from '../../../../quests/requests';
import type { QuestProgress } from '../../../../quests/types';

const polling = (result: QuestProgress) =>
  result.status === 'waiting' || result.review_pending === true || result.current_attempt?.status === 'pending_review' || result.current_attempt?.uploading === true;

function Play({ scope, id }: { scope: QuestScope; id: string }) {
  const fetchProgress = useCallback((signal: AbortSignal) => scope.withAccess(auth => api.quests.progress(id, auth), signal), [scope, id]);
  const { data, error, refresh, busy } = useQuestRead(fetchProgress, polling);
  const [seen, setSeen] = useState(0);
  const [openStory, setOpenStory] = useState<string | null>();
  const action = useQuestAction();
  const progressStatus = data?.status;
  const openedTasks = data?.stories.map(story => story.task_id).join(',') ?? '';
  const attemptID = data?.current_attempt?.id;
  const attemptStatus = data?.current_attempt?.status;

  useFocusEffect(useCallback(() => {
    if (!progressStatus || Platform.OS === 'web') return;
    let cancelled = false;
    // A confirmed server transition also retires a local request whose response was lost.
    void (async () => {
      const storage = questDraftStorage(getApiBaseUrl(), scope.owner, id);
      const saved = await storage.read();
      if (cancelled || !saved) return;
      if (progressStatus === 'completed' || progressStatus === 'closed' || openedTasks.split(',').includes(saved.task) ||
          (saved.attempt && saved.attempt === attemptID && attemptStatus !== 'draft')) await storage.write(undefined);
    })().catch(() => {});
    return () => { cancelled = true; };
  // Only rerun when the server's current task or attempt status changes.
  }, [scope.owner, id, progressStatus, openedTasks, attemptID, attemptStatus]));

  if (!data) return <QuestScreen title="Фотоквест"><QuestLoad error={error} retry={refresh} /></QuestScreen>;
  const task = data.current_task;
  const attempt = data.current_attempt;
  const last = data.stories.at(-1);
  const freshStory = data.status === 'active' && last && last.position > seen ? last : undefined;
  const selected = data.stories.find(story => story.task_id === openStory) ?? (data.status === 'completed' && openStory === undefined ? last : undefined);
  function withdraw(attemptID: string) {
    Alert.alert('Отозвать снимок?', 'Организатор больше не сможет его проверить. Вы сможете сделать другой снимок.', [
      { text: 'Отмена', style: 'cancel' },
      { text: 'Отозвать', style: 'destructive', onPress: () => void action.run(async signal => {
        await scope.withAccess(auth => api.quests.withdraw(attemptID, auth), signal);
        if (!signal.aborted) refresh();
      }) },
    ]);
  }

  return <QuestScreen title={data.status === 'completed' ? 'Квест пройден' : data.status === 'closed' ? 'Прогулка завершена' : data.title}
    description={data.status === 'completed' || data.status === 'closed' ? data.title : undefined}>
    <Text accessibilityLiveRegion="polite" style={questStyles.label}>Выполнено {data.completed_steps} из {data.total_steps}</Text>
    <QuestError error={error ?? action.error} />
    {error != null && <AppButton title="Проверить связь" loading={busy} variant="secondary" onPress={refresh} />}
    {data.status === 'waiting' && <QuestPanel title="Ждём старта">
      <Text style={questStyles.text}>Организатор ещё не начал прогулку. Первая подсказка появится здесь.</Text>
    </QuestPanel>}
    {data.status === 'closed' && <QuestPanel title="Результаты прогулки">
      <Text style={questStyles.text}>{data.review_pending ? 'Отправленная работа ещё на проверке.' : 'Организатор закрыл отправку новых работ.'}</Text>
      <Text style={questStyles.label}>За прогулку +{data.xp_awarded} XP</Text>
      {data.review_pending && <AppButton title="Обновить результат" variant="secondary" onPress={refresh} />}
    </QuestPanel>}
    {freshStory && <>
      <Text accessibilityRole="header" style={questStyles.heading}>Точка найдена!</Text>
      <QuestStory key={freshStory.task_id} story={freshStory} scope={scope} />
      <AppButton title="Следующая подсказка" onPress={() => setSeen(freshStory.position)} />
    </>}
    {!freshStory && task && <QuestPanel title={`Этап ${task.position} из ${data.total_steps}`}>
      <Text selectable style={questStyles.text}>{task.clue}</Text>
      <Text style={questStyles.label}>Задание для фото</Text>
      <Text style={questStyles.text}>{task.photo_task}</Text>
      {attempt?.status === 'pending_review' ? <>
        <Text accessibilityLiveRegion="polite" style={questStyles.label}>Фото на проверке у организатора</Text>
        <AppButton title="Обновить статус" variant="secondary" onPress={refresh} />
        <AppButton title="Отозвать фото" variant="secondary" disabled={action.busy} onPress={() => withdraw(attempt.id)} />
      </> : <>
        {(attempt?.status === 'rejected' || attempt?.status === 'expired') && <Text accessibilityLiveRegion="polite" style={questStyles.error}>
          {attempt.status === 'expired' ? 'Время проверки истекло. Сделайте новый снимок.' : attempt.rejection_reason || 'Организатор попросил переснять фото.'}
        </Text>}
        {Platform.OS === 'web' ? <Text style={questStyles.note}>Для съёмки и отправки работы откройте квест на телефоне.</Text> :
          <AppButton title={attempt?.status === 'rejected' || attempt?.status === 'expired' ? 'Переснять фото' : attempt?.photo_ready || attempt?.uploading ? 'Продолжить с фото' : 'Сделать или открыть снимок'}
            onPress={() => router.push({ pathname: '/quests/play/[participationId]/capture', params: { participationId: id } })} />}
      </>}
    </QuestPanel>}
    {data.status === 'completed' && <QuestPanel title={`За прогулку +${data.xp_awarded} XP`}>
      {data.started_at && <Text style={questStyles.note}>Начало · {historyDate(data.started_at, 'Europe/Minsk')}</Text>}
      {data.completed_at && <Text style={questStyles.note}>Финиш · {historyDate(data.completed_at, 'Europe/Minsk')}</Text>}
    </QuestPanel>}
    {!freshStory && data.stories.length > 0 && <QuestPanel title="Открытые места">
      {data.stories.map(story => <AppButton key={story.task_id} title={story.place_name}
        expanded={selected?.task_id === story.task_id} variant="secondary"
        onPress={() => setOpenStory(selected?.task_id === story.task_id ? null : story.task_id)} />)}
    </QuestPanel>}
    {!freshStory && selected && <QuestStory key={selected.task_id} story={selected} scope={scope} />}
    {(data.status === 'completed' || data.status === 'closed') && <AppButton title="Вернуться в квесты" onPress={() => router.dismissTo('/quests')} />}
  </QuestScreen>;
}

export default function PlayQuestScreen() {
  const { participationId } = useLocalSearchParams<{ participationId: string }>();
  return <QuestGate title="Фотоквест">{scope => <Play scope={scope} id={participationId ?? ''} />}</QuestGate>;
}
