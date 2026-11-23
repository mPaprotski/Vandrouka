import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useState } from 'react';
import { Alert, Text, View } from 'react-native';
import { api } from '../../../api/client';
import type { Place } from '../../../api/client';
import { QuestError, QuestField, QuestGate, QuestLoad, QuestPanel, QuestScreen, questStyles } from '../../../components/quests/QuestUI';
import { AppButton } from '../../../components/ui/AppButton';
import { useQuestAction, useQuestRead } from '../../../quests/requests';
import type { QuestScope } from '../../../quests/requests';
import type { Draft, Editor, Invite, QuestStep } from '../../../quests/types';

type Mode = 'edit' | 'preview' | 'launch';
const emptyStep = (place_id: number): QuestStep => ({ place_id, clue: '', photo_task: '', reveal_text: '' });
const label = (s: Editor['status']) => ({ draft: 'Черновик', published: 'Опубликован', archived: 'В архиве' }[s]);
function toDraft(editor: Editor): Draft {
  return { title: editor.title, description: editor.description, steps: editor.steps };
}

function QuestForm({ scope, initial }: { scope: QuestScope; initial: Editor }) {
  const [saved, setSaved] = useState(initial);
  const [draft, setDraft] = useState(() => toDraft(initial));
  const [mode, setMode] = useState<Mode>('edit');
  const [adding, setAdding] = useState(false);
  const [query, setQuery] = useState('');
  const [runTitle, setRunTitle] = useState('');
  const [invite, setInvite] = useState<Invite>();
  const action = useQuestAction();

  const loadPlaces = useCallback(async (signal: AbortSignal) => {
    const cities = await api.listCities({ signal });
    const city = cities.find(item => item.id === initial.city_id);
    if (!city) throw new Error('Город этого маршрута недоступен.');
    return api.listPlaces(city.slug, {}, { signal });
  }, [initial.city_id]);
  const catalog = useQuestRead(loadPlaces);
  const places = catalog.data ?? [];
  const name = (id: number) => places.find(place => place.id === id)?.name ?? 'Место недоступно';
  const dirty = JSON.stringify(draft) !== JSON.stringify(toDraft(saved));
  const editable = saved.status !== 'archived' && !action.busy;
  const available = places.filter(place => !draft.steps.some(step => step.place_id === place.id));
  const matching = available.filter(place => place.name.toLowerCase().includes(query.trim().toLowerCase()));
  const complete = [...draft.title.trim()].length >= 3 && draft.steps.length >= 2 &&
    draft.steps.every(step => step.clue.trim() && step.photo_task.trim() && step.reveal_text.trim());

  function change(place: number, field: 'clue' | 'photo_task' | 'reveal_text', value: string) {
    setDraft(old => ({ ...old, steps: old.steps.map(step => step.place_id === place ? { ...step, [field]: value } : step) }));
  }
  function move(index: number, by: number) {
    setDraft(old => {
      const steps = [...old.steps];
      [steps[index], steps[index + by]] = [steps[index + by], steps[index]];
      return { ...old, steps };
    });
  }
  function add(place: Place) {
    setDraft(old => ({ ...old, steps: [...old.steps, emptyStep(place.id)] }));
    setQuery('');
    setAdding(false);
  }
  function reload() {
    const load = () => void action.run(async signal => {
      const editor = await scope.withAccess(auth => api.quests.editor(saved.id, auth), signal);
      if (signal.aborted) return;
      setSaved(editor);
      setDraft(toDraft(editor));
    });
    if (!dirty) load();
    else Alert.alert('Загрузить сохранённую редакцию?', 'Введённые изменения будут потеряны.', [
      { text: 'Отмена', style: 'cancel' }, { text: 'Загрузить', onPress: load },
    ]);
  }

  if (mode === 'preview') return <QuestScreen title={draft.title || 'Предпросмотр'} description={draft.description}>
    {draft.steps.map((step, index) => <QuestPanel key={step.place_id} title={`Этап ${index + 1}. ${name(step.place_id)}`}>
      <Text style={questStyles.label}>Подсказка</Text><Text style={questStyles.text}>{step.clue || '—'}</Text>
      <Text style={questStyles.label}>Задание для фото</Text><Text style={questStyles.text}>{step.photo_task || '—'}</Text>
      <Text style={questStyles.label}>После принятия</Text><Text style={questStyles.text}>{step.reveal_text || '—'}</Text>
    </QuestPanel>)}
    <AppButton title="Вернуться к маршруту" onPress={() => setMode('edit')} />
  </QuestScreen>;

  if (mode === 'launch') return <QuestScreen title="Создать запуск" description={saved.title}>
    {invite ? <>
      <QuestPanel title="Код для участников">
        <Text selectable style={questStyles.code}>{invite.join_code}</Text>
        <Text style={questStyles.note}>Передайте код группе. Первая подсказка откроется после старта.</Text>
      </QuestPanel>
      <AppButton title="Управлять прогулкой" onPress={() => router.replace({ pathname: '/quests/run/[runId]', params: { runId: invite.id } })} />
    </> : <>
      <QuestField label="Название группы или прогулки" placeholder="Например, 7Б" value={runTitle} onChangeText={setRunTitle} maxLength={120} />
      <QuestError error={action.error} />
      {action.error != null && <Text style={questStyles.note}>Если ответ потерялся, проверьте список «Организую» перед повтором.</Text>}
      <AppButton title="Создать запуск" disabled={[...runTitle.trim()].length < 3} loading={action.busy}
        onPress={() => void action.run(async signal => {
          const result = await scope.withAccess(auth => api.quests.launch(saved.id, saved.revision, runTitle.trim(), auth), signal);
          if (!signal.aborted) setInvite(result);
        })} />
      <AppButton title="К маршруту" variant="secondary" disabled={action.busy} onPress={() => setMode('edit')} />
    </>}
  </QuestScreen>;

  return <QuestScreen title={draft.title || 'Маршрут квеста'} description={label(saved.status)}>
    <QuestField label="Название маршрута" value={draft.title} editable={editable} maxLength={120}
      onChangeText={title => setDraft(old => ({ ...old, title }))} />
    <QuestField label="Описание маршрута" value={draft.description} editable={editable} maxLength={3000} multiline
      onChangeText={description => setDraft(old => ({ ...old, description }))} />
    {!catalog.data && <QuestLoad error={catalog.error} retry={catalog.refresh} />}
    {draft.steps.map((step, index) => <QuestPanel key={step.place_id} title={`${index + 1}. ${name(step.place_id)}`}>
      <QuestField label="Подсказка" value={step.clue} editable={editable} maxLength={1000} multiline
        onChangeText={text => change(step.place_id, 'clue', text)} />
      <QuestField label="Задание для фотографии" value={step.photo_task} editable={editable} maxLength={1000} multiline
        onChangeText={text => change(step.place_id, 'photo_task', text)} />
      <QuestField label="Рассказ после выполнения" value={step.reveal_text} editable={editable} maxLength={3000} multiline
        onChangeText={text => change(step.place_id, 'reveal_text', text)} />
      {saved.status !== 'archived' && <View style={{ gap: 8 }}>
        <AppButton title="Выше" accessibilityLabel={`Поднять ${name(step.place_id)}`} variant="secondary" disabled={!editable || index === 0} onPress={() => move(index, -1)} />
        <AppButton title="Ниже" accessibilityLabel={`Опустить ${name(step.place_id)}`} variant="secondary" disabled={!editable || index === draft.steps.length - 1} onPress={() => move(index, 1)} />
        <AppButton title="Убрать из маршрута" variant="secondary" disabled={!editable}
          onPress={() => setDraft(old => ({ ...old, steps: old.steps.filter(item => item.place_id !== step.place_id) }))} />
      </View>}
    </QuestPanel>)}
    {adding && <QuestPanel title="Места из каталога">
      <QuestField label="Поиск по названию" value={query} onChangeText={setQuery} />
      {!matching.length && <Text style={questStyles.note}>Подходящих мест нет.</Text>}
      {matching.slice(0, 12).map(place => <AppButton key={place.id} title={place.name} variant="secondary" onPress={() => add(place)} />)}
      {matching.length > 12 && <Text style={questStyles.note}>Уточните поиск, чтобы увидеть остальные места.</Text>}
      <AppButton title="Закрыть список" variant="secondary" onPress={() => setAdding(false)} />
    </QuestPanel>}
    {!adding && saved.status !== 'archived' && <AppButton title="Добавить место" variant="secondary"
      disabled={!editable || !catalog.data || draft.steps.length >= 20} onPress={() => setAdding(true)} />}
    <QuestError error={action.error} />
    {saved.status !== 'archived' && <>
      {dirty && saved.status === 'published' && <Text style={questStyles.note}>После сохранения опубликуйте новую редакцию для будущих запусков.</Text>}
      <AppButton title="Сохранить черновик" disabled={!editable || !dirty} loading={action.busy} onPress={() => void action.run(async signal => {
        const value = await scope.withAccess(auth => api.quests.save(saved.id, saved.revision, draft, auth), signal);
        if (!signal.aborted) { setSaved(value); setDraft(toDraft(value)); }
      })} />
      {saved.status === 'draft' && <AppButton title="Опубликовать" disabled={!editable || dirty || !complete} onPress={() =>
        void action.run(async signal => {
          const quest = await scope.withAccess(auth => api.quests.publish(saved.id, saved.revision, auth), signal);
          if (!signal.aborted) setSaved({ ...saved, ...quest });
        })} />}
      {saved.status === 'draft' && !complete && <Text style={questStyles.note}>Для публикации заполните название и три текста как минимум для двух мест.</Text>}
      {saved.status === 'published' && <AppButton title="Создать запуск" disabled={dirty || action.busy}
        onPress={() => { setInvite(undefined); setRunTitle(''); setMode('launch'); }} />}
    </>}
    {!!draft.steps.length && <AppButton title="Предпросмотр" variant="secondary" disabled={action.busy} onPress={() => setMode('preview')} />}
    {action.error != null && <AppButton title="Загрузить сохранённую редакцию" variant="secondary" disabled={action.busy} onPress={reload} />}
  </QuestScreen>;
}

function EditorScreen({ scope, id }: { scope: QuestScope; id: string }) {
  const load = useCallback((signal: AbortSignal) => scope.withAccess(auth => api.quests.editor(id, auth), signal), [scope, id]);
  const { data, error, refresh } = useQuestRead(load);
  return data ? <QuestForm key={data.id} scope={scope} initial={data} /> :
    <QuestScreen title="Маршрут квеста"><QuestLoad error={error} retry={refresh} /></QuestScreen>;
}
export default function EditQuest() {
  const { questId } = useLocalSearchParams<{ questId: string }>();
  return <QuestGate title="Маршрут квеста">{scope => <EditorScreen scope={scope} id={questId ?? ''} />}</QuestGate>;
}
