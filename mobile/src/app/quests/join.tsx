import { router } from 'expo-router';
import { useState } from 'react';
import { api } from '../../api/client';
import { QuestError, QuestField, QuestGate, QuestScreen } from '../../components/quests/QuestUI';
import { AppButton } from '../../components/ui/AppButton';
import { useQuestAction } from '../../quests/requests';
import type { QuestScope } from '../../quests/requests';

function Join({ scope }: { scope: QuestScope }) {
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const joining = useQuestAction();
  return <QuestScreen title="Вход в квест" description="Организатор даст вам код прогулки.">
    <QuestField label="Код прогулки" value={code} onChangeText={text => setCode(text.replace(/\s+/g, '').toUpperCase())}
      autoCapitalize="characters" autoCorrect={false} maxLength={8} />
    <QuestField label="Имя для группы" value={name} onChangeText={setName} maxLength={60} />
    <QuestError error={joining.error} />
    <AppButton title="Присоединиться" loading={joining.busy} disabled={!/^[A-HJ-NP-Z2-9]{8}$/.test(code) || !name.trim()}
      onPress={() => void joining.run(async signal => {
        const result = await scope.withAccess(auth => api.quests.join(code, name.trim(), auth), signal);
        if (!signal.aborted) router.replace({ pathname: '/quests/play/[participationId]', params: { participationId: result.id } });
      })} />
  </QuestScreen>;
}

export default function JoinQuest() {
  return <QuestGate title="Вход в квест">{scope => <Join scope={scope} />}</QuestGate>;
}
