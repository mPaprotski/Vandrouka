import { useLocalSearchParams } from 'expo-router';
import { useCallback, useState } from 'react';
import { Alert, Text } from 'react-native';
import { api } from '../../../api/client';
import { QuestList } from '../../../components/quests/QuestLists';
import { QuestPhoto } from '../../../components/quests/QuestPhoto';
import { QuestError, QuestField, QuestGate, QuestLoad, QuestPanel, QuestScreen, questStyles } from '../../../components/quests/QuestUI';
import { AppButton } from '../../../components/ui/AppButton';
import { historyDate } from '../../../history/photo';
import { useQuestAction, useQuestRead } from '../../../quests/requests';
import type { QuestScope } from '../../../quests/requests';
import type { Review } from '../../../quests/types';

function ReviewCard({ item, scope, done, back }: { item: Review; scope: QuestScope; done: () => void; back: () => void }) {
  const [reason, setReason] = useState('');
  const [seen, setSeen] = useState(false);
  const action = useQuestAction();
  function review(decision: 'accept' | 'reject') {
    void action.run(async signal => {
      await scope.withAccess(auth => api.quests.review(item.id, item.revision, decision, reason.trim(), auth), signal);
      if (!signal.aborted) done();
    });
  }
  return <QuestPanel title={`${item.participant_name} · этап ${item.position}`}>
    <Text style={questStyles.label}>{item.place_name}</Text>
    <Text style={questStyles.text}>{item.photo_task}</Text>
    {item.submitted_at && <Text style={questStyles.note}>{historyDate(item.submitted_at, 'Europe/Minsk')}</Text>}
    <Text style={questStyles.note}>GPS подтверждён · {Math.round(item.distance_m)} м до места</Text>
    <QuestPhoto id={item.id} scope={scope} onReady={setSeen} />
    <QuestError error={action.error} />
    <AppButton title="Принять снимок" loading={action.busy} disabled={!seen} onPress={() => review('accept')} />
    <QuestField label="Комментарий при отклонении" value={reason} onChangeText={setReason} maxLength={1000} multiline />
    <AppButton title="Отклонить и попросить переснять" variant="secondary" disabled={action.busy || !reason.trim()} onPress={() => review('reject')} />
    <AppButton title="К очереди" variant="secondary" disabled={action.busy} onPress={back} />
  </QuestPanel>;
}

function ManageRun({ scope, id }: { scope: QuestScope; id: string }) {
  const load = useCallback((signal: AbortSignal) => scope.withAccess(auth => api.quests.manage(id, auth), signal), [scope, id]);
  const run = useQuestRead(load);
  const participants = useCallback((signal: AbortSignal, cursor?: string) =>
    scope.withAccess(auth => api.quests.participants(id, auth, cursor), signal), [scope, id]);
  const queue = useCallback((signal: AbortSignal, cursor?: string) =>
    scope.withAccess(auth => api.quests.queue(id, auth, cursor), signal), [scope, id]);
  const [page, setPage] = useState<'participants' | 'queue'>('participants');
  const [selected, setSelected] = useState<Review>();
  const [code, setCode] = useState<string>();
  const action = useQuestAction();

  if (!run.data) return <QuestScreen title="Управление прогулкой"><QuestLoad error={run.error} retry={run.refresh} /></QuestScreen>;
  const data = run.data;
  const active = data.status === 'active';
  const finished = data.status === 'finished';
  function finish() {
    Alert.alert('Завершить прогулку?', 'Новые работы больше нельзя будет отправить. Снимки в очереди останутся доступны для проверки.', [
      { text: 'Отмена', style: 'cancel' },
      { text: 'Завершить', onPress: () => void action.run(async signal => {
        await scope.withAccess(auth => api.quests.finish(id, auth), signal);
        if (!signal.aborted) { setCode(undefined); run.refresh(); }
      }) },
    ]);
  }
  return <QuestScreen title={data.title} description={finished ? 'Прогулка завершена' : active ? `Прогулка началась · ${data.total_steps} этапов` : 'Ожидает старта'}>
    {selected ? <ReviewCard key={selected.id} item={selected} scope={scope}
      done={() => { setSelected(undefined); run.refresh(); }} back={() => setSelected(undefined)} /> : <>
      {!finished && <QuestPanel title="Приглашения">
        {code && <Text selectable style={questStyles.code}>{code}</Text>}
        <Text style={questStyles.note}>{data.join_enabled ? 'Вход по коду открыт.' : 'Новые участники пока не могут присоединиться.'}</Text>
        <AppButton title="Выпустить новый код" variant="secondary" disabled={action.busy}
          onPress={() => void action.run(async signal => {
            const invite = await scope.withAccess(auth => api.quests.rotate(id, auth), signal);
            if (!signal.aborted) { setCode(invite.join_code); run.refresh(); }
          })} />
        <AppButton title={data.join_enabled ? 'Закрыть вход' : 'Открыть вход'} variant="secondary" disabled={action.busy}
          onPress={() => void action.run(async signal => {
            await scope.withAccess(auth => api.quests.joining(id, !data.join_enabled, auth), signal);
            if (!signal.aborted) run.refresh();
          })} />
      </QuestPanel>}
      {data.status === 'waiting' && <AppButton title="Начать квест" loading={action.busy} onPress={() => void action.run(async signal => {
        await scope.withAccess(auth => api.quests.start(id, auth), signal);
        if (!signal.aborted) run.refresh();
      })} />}
      {active && <AppButton title="Завершить прогулку" variant="secondary" disabled={action.busy} onPress={finish} />}
      {finished && <Text style={questStyles.note}>Отправленные до закрытия снимки можно проверить в очереди.</Text>}
      <QuestError error={action.error ?? run.error} />
      {page === 'participants' ? <>
        <AppButton title="Перейти к проверке фото" variant="secondary" onPress={() => setPage('queue')} />
        <QuestList title="Участники" empty="Пока никто не присоединился" load={participants} live render={item =>
          <Text key={item.id} style={questStyles.text}>{item.display_name} · {item.completed_steps} из {data.total_steps}</Text>} />
      </> : <>
        <AppButton title="Посмотреть участников" variant="secondary" onPress={() => setPage('participants')} />
        <QuestList title="Снимки на проверке" empty="Сейчас нет работ на проверке" load={queue} live render={item =>
          <AppButton key={item.id} title={`${item.participant_name} · этап ${item.position}`} variant="secondary" onPress={() => setSelected(item)} />} />
      </>}
    </>}
  </QuestScreen>;
}
export default function QuestRunScreen() {
  const { runId } = useLocalSearchParams<{ runId: string }>();
  return <QuestGate title="Управление прогулкой">{scope => <ManageRun scope={scope} id={runId ?? ''} />}</QuestGate>;
}
