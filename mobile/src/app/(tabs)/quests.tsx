import { router } from 'expo-router';
import { useCallback } from 'react';
import { api } from '../../api/client';
import { QuestList } from '../../components/quests/QuestLists';
import { QuestGate, QuestScreen } from '../../components/quests/QuestUI';
import { AppButton } from '../../components/ui/AppButton';
import type { QuestScope } from '../../quests/requests';

function MyQuests({ scope }: { scope: QuestScope }) {
  const loadPlay = useCallback((signal: AbortSignal, cursor?: string) =>
    scope.withAccess(auth => api.quests.participations(auth, cursor), signal), [scope]);
  const loadRuns = useCallback((signal: AbortSignal, cursor?: string) =>
    scope.withAccess(auth => api.quests.runs(auth, cursor), signal), [scope]);
  const loadRoutes = useCallback((signal: AbortSignal, cursor?: string) =>
    scope.withAccess(auth => api.quests.templates(auth, cursor), signal), [scope]);

  return <QuestScreen title="Фотоквесты" description="Ищите места по подсказкам или создайте свою прогулку.">
    <AppButton title="Присоединиться по коду" onPress={() => router.push('/quests/join')} />
    <AppButton title="Создать квест" variant="secondary" onPress={() => router.push('/quests/new')} />
    <QuestList title="Прохожу" empty="Здесь будут ваши прогулки" load={loadPlay} render={item =>
      <AppButton key={item.id} variant="secondary"
        title={`${item.title} · ${item.status === 'waiting' ? 'Ждём старта' : item.status === 'closed' ? 'Прогулка закрыта' : `${item.completed_steps} из ${item.total_steps}`}`}
        onPress={() => router.push({ pathname: '/quests/play/[participationId]', params: { participationId: item.id } })} />}
    />
    <QuestList title="Организую" empty="Здесь будут запуски для групп" load={loadRuns} render={item =>
      <AppButton key={item.id} variant="secondary" title={`${item.title} · ${{ waiting: 'Ожидает старта', active: 'Идёт', finished: 'Завершён' }[item.status]}`}
        onPress={() => router.push({ pathname: '/quests/run/[runId]', params: { runId: item.id } })} />}
    />
    <QuestList title="Мои маршруты" empty="Здесь будут ваши шаблоны" load={loadRoutes} render={item =>
      <AppButton key={item.id} variant="secondary" title={`${item.title || 'Без названия'} · ${{ draft: 'Черновик', published: 'Опубликован', archived: 'Архив' }[item.status]}`}
        onPress={() => router.push({ pathname: '/quests/edit/[questId]', params: { questId: item.id } })} />}
    />
  </QuestScreen>;
}

export default function QuestTab() {
  return <QuestGate title="Фотоквесты">{scope => <MyQuests scope={scope} />}</QuestGate>;
}
