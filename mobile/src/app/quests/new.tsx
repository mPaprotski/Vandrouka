import { router } from 'expo-router';
import { useState } from 'react';
import { api } from '../../api/client';
import { AppButton } from '../../components/ui/AppButton';
import { QuestError, QuestGate, QuestLoad, QuestScreen } from '../../components/quests/QuestUI';
import { useQuestAction, useQuestRead } from '../../quests/requests';
import type { QuestScope } from '../../quests/requests';

const cities = (signal: AbortSignal) => api.listCities({ signal });
function NewQuest({ scope }: { scope: QuestScope }) {
  const { data, error, refresh } = useQuestRead(cities);
  const [cityID, setCityID] = useState<number>();
  const action = useQuestAction();
  const chosen = cityID ?? (data?.length === 1 ? data[0].id : undefined);
  return <QuestScreen title="Новый квест" description="Выберите город, в котором проходит прогулка.">
    {!data ? <QuestLoad error={error} retry={refresh} /> : data.map(city =>
      <AppButton key={city.id} title={city.name} variant={chosen === city.id ? 'primary' : 'secondary'}
        disabled={action.busy} onPress={() => setCityID(city.id)} />)}
    <QuestError error={action.error} />
    <AppButton title="Создать черновик" disabled={chosen === undefined} loading={action.busy}
      onPress={() => void action.run(async signal => {
        if (chosen === undefined) return;
        const draft = await scope.withAccess(auth => api.quests.create(chosen, auth), signal);
        if (!signal.aborted) router.replace({ pathname: '/quests/edit/[questId]', params: { questId: draft.id } });
      })} />
  </QuestScreen>;
}
export default function NewQuestScreen() {
  return <QuestGate title="Новый квест">{scope => <NewQuest scope={scope} />}</QuestGate>;
}
