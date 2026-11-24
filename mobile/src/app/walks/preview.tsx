import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Text, TextInput, View } from 'react-native';
import { api, type Place } from '../../api/client';
import { ApiError } from '../../api/errors';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { PlacesMap } from '../../components/PlacesMap';
import { WalkVariantCard } from '../../components/walks/WalkVariantCard';
import { WalkStopList } from '../../components/walks/WalkStopList';
import { StartWalkButton } from '../../components/walks/StartWalkButton';
import { useWalkPlanner } from '../../walks/WalkProvider';
import { distance, duration, walkTime } from '../../walks/format';
import { mapWalk } from '../../walks/geometry';
import { getMapboxToken } from '../../map/document';
import { useForegroundLocation } from '../../hooks/useForegroundLocation';
import type { RecalculatedWalk, WalkInput, WalkVariant } from '../../walks/types';

export default function WalkPreview() {
  const { variantId } = useLocalSearchParams<{ variantId?: string }>();
  const { calculation, setCalculation } = useWalkPlanner();
  const variant = calculation?.result.variants.find(v => v.variant_id === variantId);
  if (!calculation || !variant) return <SectionScreen title="Маршрут недоступен">
    <Text>Постройте прогулку, чтобы посмотреть её остановки.</Text>
    <AppButton title="Параметры прогулки" onPress={() => router.replace('/walks')} />
  </SectionScreen>;
  return <Preview key={`${variantId}:${calculation.request.start_at}`} variant={variant} request={calculation.request} timezone={calculation.result.timezone}
    onApply={proposal => setCalculation({
      request: { ...calculation.request, seed: proposal.seed },
      result: { ...calculation.result, seed: proposal.seed, timezone: proposal.timezone,
        variants: calculation.result.variants.map(v => v.variant_id === variantId ? { ...proposal.variant, variant_id: v.variant_id } : { ...v, labels: [] }) },
    })} />;
}

function Preview({ variant, request, timezone, onApply }: {
  variant: WalkVariant; request: WalkInput; timezone: string; onApply: (proposal: RecalculatedWalk) => void;
}) {
  const [view, setView] = useState<'map' | 'list'>('list');
  const [selectedId, setSelectedId] = useState<number>();
  const [proposal, setProposal] = useState<RecalculatedWalk>();
  const [catalog, setCatalog] = useState<Place[]>();
  const [choosing, setChoosing] = useState(false);
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [requestId, setRequestId] = useState<string>();
  const [cooldown, setCooldown] = useState(0);
  const pending = useRef<AbortController | null>(null);
  const location = useForegroundLocation();
  useEffect(() => () => pending.current?.abort(), []);
  useEffect(() => {
    if (!cooldown) return;
    const timer = setTimeout(() => setCooldown(0), Math.max(0, cooldown - Date.now()));
    return () => clearTimeout(timer);
  }, [cooldown]);
  const displayed = proposal?.variant ?? variant;
  const displayedTimezone = proposal?.timezone ?? timezone;
  const shape = useMemo(() => mapWalk(request, displayed, selectedId), [request, displayed, selectedId]);
  const places = useMemo(() => displayed.stops.map((s, i) => ({ id: s.place_id, name: s.name, lat: s.location.lat, lon: s.location.lon, order: i + 1, selected: s.place_id === selectedId })), [displayed, selectedId]);
  const blocked = busy || cooldown !== 0;

  async function perform(work: (signal: AbortSignal) => Promise<void>, loading: string) {
    if (pending.current || blocked || proposal) return;
    const abort = new AbortController(); pending.current = abort;
    setBusy(true); setMessage(loading); setRequestId(undefined);
    try { await work(abort.signal); }
    catch (error) {
      if (abort.signal.aborted) return;
      const e = error instanceof ApiError ? error : undefined;
      const messages: Record<string, string> = {
        constraints_violation: 'Изменение не помещается в ограничения прогулки. Выберите другое место или измените параметры.',
        not_found: 'Одно из мест стало недоступно. Обновите каталог или постройте прогулку заново.',
        routing_unavailable: 'Сервис маршрутов временно недоступен. Повторите действие позже.',
        rate_limited: 'Слишком много запросов. Подождите перед повтором.',
      };
      setMessage(messages[e?.code ?? ''] ?? (error instanceof Error ? error.message : 'Не удалось выполнить действие.'));
      setRequestId(e?.requestId);
      if (e?.status === 429) setCooldown(Date.now() + (e.retryAfterMs ?? 1000));
    } finally { if (pending.current === abort) { pending.current = null; setBusy(false); } }
  }
  function loadCatalog() {
    setChoosing(true);
    void perform(async signal => {
      const data = await api.listPlaces(request.city, {}, { signal });
      if (!signal.aborted) { setCatalog(data); setMessage(''); }
    }, 'Загружаем места…');
  }
  function change(type: 'add' | 'remove', placeId: number) {
    void perform(async signal => {
      const data = await api.walks.recalculate({ request, base_place_ids: variant.stops.map(s => s.place_id), change: { type, place_id: placeId } }, { signal });
      if (!signal.aborted) { setProposal(data); setSelectedId(undefined); setChoosing(false); setMessage(''); }
    }, 'Пересчитываем путь и расписание…');
  }
  const candidates = catalog?.filter(p => !variant.stops.some(s => s.place_id === p.id) && p.name.toLocaleLowerCase('ru-RU').includes(query.trim().toLocaleLowerCase('ru-RU')));
  const selected = displayed.stops.find(s => s.place_id === selectedId);
  return <SectionScreen title={proposal ? 'Предлагаемое изменение' : 'Маршрут прогулки'}>
    {proposal && <>
      <Text accessibilityLiveRegion="polite">{proposal.diff.text}</Text>
      <Text>Время — {proposal.diff.total_seconds_delta >= 0 ? '+' : '−'}{duration(Math.abs(proposal.diff.total_seconds_delta))}. Расстояние — {proposal.diff.distance_m_delta >= 0 ? '+' : '−'}{distance(Math.abs(proposal.diff.distance_m_delta))}.</Text>
      <Text>{proposal.diff.prices_confirmed ? `Изменение стоимости — ${(proposal.diff.cost_minor_delta / 100).toLocaleString('ru-RU')} ${proposal.variant.totals.currency}` : 'Стоимость известна не для всех мест.'}</Text>
      {proposal.diff.added_place_ids.length > 0 && <Text>Добавлены — {proposal.diff.added_place_ids.map(id => proposal.variant.stops.find(s => s.place_id === id)?.name ?? `Место ${id}`).join(', ')}</Text>}
      {proposal.diff.removed_place_ids.length > 0 && <Text>Убраны — {proposal.diff.removed_place_ids.map(id => variant.stops.find(s => s.place_id === id)?.name ?? `Место ${id}`).join(', ')}</Text>}
      <AppButton title="Применить изменение" onPress={() => { onApply(proposal); setProposal(undefined); }} />
      <AppButton title="Оставить прежний маршрут" variant="secondary" onPress={() => { setProposal(undefined); setSelectedId(undefined); }} />
    </>}
    <WalkVariantCard variant={displayed} title="Ваша прогулка" />
    <Text>{request.travel_mode==='cycling'?'На велосипеде':'Пешком'}{request.max_distance_m!==undefined?` · До ${distance(request.max_distance_m)}`:''}{request.max_stops!==undefined?` · До ${request.max_stops} мест`:''}</Text>
    {!proposal && !busy && <StartWalkButton request={request} variant={variant} timezone={timezone} />}
    <Text>Старт — {walkTime(request.start_at, displayedTimezone)}. Время указано для города прогулки.</Text>
    <View style={{ flexDirection: 'row', gap: 12 }}>
      <AppButton title="Карта" variant={view === 'map' ? 'primary' : 'secondary'} onPress={() => setView('map')} />
      <AppButton title="Список" variant={view === 'list' ? 'primary' : 'secondary'} onPress={() => setView('list')} />
    </View>
    {view === 'map' && <>
      {shape.incomplete && <Text>Часть пути недоступна. Остановки можно посмотреть на карте и в списке.</Text>}
      <View style={{ height: 440 }}><PlacesMap token={getMapboxToken()} places={places} route={shape.route}
        position={location.state.status === 'success' ? location.state.fix : null} onSelect={setSelectedId} onShowList={() => setView('list')} /></View>
      <AppButton title="Определить моё положение" variant="secondary" onPress={() => void location.locate()} />
      {location.state.status === 'loading' && <Text>Определяем местоположение…</Text>}
      {location.state.status === 'error' && <Text>{location.state.message}</Text>}
      {selected && <><Text>Остановка {displayed.stops.indexOf(selected) + 1} — {selected.name}</Text>
        <AppButton title="Открыть место" onPress={() => router.push({ pathname: '/places/[id]', params: { id: selected.place_id } })} />
      </>}
    </>}
    {view === 'list' && <WalkStopList stops={displayed.stops} timezone={displayedTimezone} selectedId={selectedId}
      onSelect={id => { setSelectedId(id); setView('map'); }} disabled={blocked}
      onRemove={proposal ? undefined : id => change('remove', id)} />}
    {!proposal && <AppButton title={choosing ? 'Обновить каталог' : 'Добавить остановку'} disabled={blocked} onPress={loadCatalog} />}
    {choosing && !proposal && <>
      <TextInput accessibilityLabel="Найти место для добавления" placeholder="Название места" value={query} onChangeText={setQuery} editable={!busy} style={{ minHeight: 48 }} />
      {candidates?.map(place => <AppButton key={place.id} title={`Добавить: ${place.name}`} variant="secondary" disabled={blocked} onPress={() => change('add', place.id)} />)}
      {candidates?.length === 0 && <Text>Подходящих мест в каталоге нет. Измените поиск.</Text>}
      <AppButton title="Закрыть выбор места" variant="secondary" disabled={busy} onPress={() => setChoosing(false)} />
    </>}
    {message ? <Text accessibilityLiveRegion="polite">{message}</Text> : null}
    {requestId && <Text>Код обращения — {requestId}</Text>}
    {cooldown !== 0 && <Text>Повтор будет доступен после {new Date(cooldown).toLocaleTimeString('ru-RU')}.</Text>}
    {busy && <AppButton title="Отменить запрос" variant="secondary" onPress={() => { pending.current?.abort(); pending.current = null; setBusy(false); setMessage('Запрос отменён.'); }} />}
    <AppButton title="Другие варианты" variant="secondary" disabled={busy || !!proposal} onPress={() => router.replace('/walks/variants')} />
    <AppButton title="Изменить параметры" variant="secondary" disabled={busy || !!proposal} onPress={() => router.replace('/walks')} />
  </SectionScreen>;
}
