import { useCallback, useState } from 'react';
import { Image } from 'react-native';
import { api } from '../../api/client';
import { privatePhotoDataUri } from '../../history/photo';
import { usePrivateResource } from '../../hooks/usePrivateResource';
import type { QuestScope } from '../../quests/requests';
import { ErrorState, LoadingState } from '../ui/ContentState';
import { questStyles } from './QuestUI';

export function QuestPhoto({ id, scope, onReady }: { id: string; scope: QuestScope; onReady?: (ready: boolean) => void }) {
  const [failed, setFailed] = useState(false);
  const load = useCallback(async (signal: AbortSignal) => {
    const blob = await scope.withAccess(auth => api.quests.photo(id, auth), signal);
    return privatePhotoDataUri(blob, signal);
  }, [scope, id]);
  const { state, retry } = usePrivateResource(load);
  if (state.status === 'loading' || state.status === 'idle') return <LoadingState message="Открываем обработанный снимок…" />;
  if (state.status === 'error' || failed) return <ErrorState
    message={state.status === 'error' ? state.message : 'Не удалось показать обработанный снимок.'}
    requestId={state.status === 'error' ? state.requestId : undefined}
    onRetry={() => { setFailed(false); onReady?.(false); retry(); }} />;
  return <Image source={{ uri: state.data }} style={questStyles.photo} accessibilityLabel="Обработанный снимок задания"
    resizeMode="contain" resizeMethod="resize" onLoad={() => onReady?.(true)}
    onError={() => { setFailed(true); onReady?.(false); }} />;
}
