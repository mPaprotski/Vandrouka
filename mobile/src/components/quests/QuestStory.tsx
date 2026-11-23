import { useState } from 'react';
import { Linking, Text } from 'react-native';
import type { QuestScope } from '../../quests/requests';
import type { Story } from '../../quests/types';
import { AppButton } from '../ui/AppButton';
import { QuestPhoto } from './QuestPhoto';
import { QuestError, QuestPanel, questStyles } from './QuestUI';

function sourceURL(input: string) {
  try {
    const url = new URL(input);
    if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) return url.href;
  } catch { /* An invalid catalog link is not actionable. */ }
  return undefined;
}

export function QuestStory({ story, scope }: { story: Story; scope: QuestScope }) {
  const [showPhoto, setShowPhoto] = useState(false);
  const [error, setError] = useState<unknown>();
  const links = story.links.flatMap(link => {
    const url = sourceURL(link.url);
    return url ? [{ title: link.title || url, url }] : [];
  });
  return <QuestPanel title={story.place_name}>
    {story.address && <Text style={questStyles.note}>{story.address}</Text>}
    <Text style={questStyles.text}>{story.text}</Text>
    <Text style={questStyles.label}>{story.xp_awarded ? '+100 XP' : 'Точка засчитана. Это место уже было открыто.'}</Text>
    <AppButton title={showPhoto ? 'Скрыть фото' : 'Показать своё фото'} variant="secondary" onPress={() => setShowPhoto(old => !old)} />
    {showPhoto && <QuestPhoto id={story.attempt_id} scope={scope} />}
    {links.map(link => <AppButton key={link.url} title={link.title} variant="secondary" onPress={() => {
      setError(undefined);
      void Linking.openURL(link.url).catch(() => setError(new Error('Не удалось открыть источник.')));
    }} />)}
    <QuestError error={error} />
  </QuestPanel>;
}
