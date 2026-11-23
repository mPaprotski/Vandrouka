import { useCallback, useState } from 'react';
import type { ReactNode } from 'react';
import { Text } from 'react-native';
import type { Page } from '../../quests/types';
import { useQuestRead } from '../../quests/requests';
import { AppButton } from '../ui/AppButton';
import { QuestError, QuestLoad, QuestPanel, questStyles } from './QuestUI';

export function QuestList<T extends { id: string }>({ title, empty, load, render, live = false }: {
  title: string; empty: string; load: (signal: AbortSignal, cursor?: string) => Promise<Page<T>>;
  render: (item: T) => ReactNode; live?: boolean;
}) {
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const cursor = cursors.at(-1);
  const fetchPage = useCallback((signal: AbortSignal) => load(signal, cursor), [cursor, load]);
  const poll = useCallback(() => live, [live]);
  const { data, error, busy, refresh } = useQuestRead(fetchPage, poll);
  return <QuestPanel title={title}>
    {!data ? <QuestLoad error={error} retry={refresh} /> : <>
      {!data.items.length && <Text style={questStyles.note}>{empty}</Text>}
      {data.items.map(render)}
      <QuestError error={error} />
      {error != null && <AppButton title="Обновить список" variant="secondary" onPress={refresh} />}
      {cursors.length > 1 && <AppButton title="Предыдущая страница" variant="secondary" disabled={busy}
        onPress={() => setCursors(old => old.slice(0, -1))} />}
      {data.next_cursor && <AppButton title="Следующая страница" variant="secondary" disabled={busy}
        onPress={() => setCursors(old => [...old, data.next_cursor])} />}
    </>}
  </QuestPanel>;
}
