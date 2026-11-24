import { router, useFocusEffect } from 'expo-router';
import { useCallback, useMemo, useRef, useState } from 'react';
import { Text, View } from 'react-native';
import { useSession } from '../../auth/SessionProvider';
import { clearWalks, walkStorage } from '../../walks/storage';
import { getApiBaseUrl } from '../../config/api';
import { mapWalk } from '../../walks/geometry';
import { getMapboxToken } from '../../map/document';
import { PlacesMap } from '../../components/PlacesMap';
import { elapsedWalk, type SavedWalk } from '../../walks/session';
import { duration, distance } from '../../walks/format';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { WalkStopList } from '../../components/walks/WalkStopList';

export default function ActiveWalk() {
  const { state } = useSession();
  if (state.status !== 'authenticated') return <SectionScreen title="Моя прогулка"><Text>Для продолжения войдите в свой аккаунт.</Text><AppButton title="Войти" onPress={() => router.push('/auth')} /></SectionScreen>;
  return <OwnedWalk key={state.user.id} owner={state.user.id} />;
}
function OwnedWalk({ owner }: { owner: number }) {
  const [walk, setWalk] = useState<SavedWalk | null>(null), [loading, setLoading] = useState(true);
  const [error, setError] = useState(''), [busy, setBusy] = useState(false), [finishing, setFinishing] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const saving = useRef(false);
  const [showMap, setShowMap] = useState(false);
  const load = useCallback(async () => {
    setLoading(true); setError('');
    try { setWalk(await walkStorage(owner).read()); }
    catch (e) { setError(e instanceof Error ? e.message : 'Не удалось восстановить прогулку.'); }
    finally { setLoading(false); }
  }, [owner]);
  useFocusEffect(useCallback(() => {
    let active = true;
    void walkStorage(owner).read().then(value => { if (active) { setWalk(value); setError(''); setLoading(false); } }).catch(e => { if (active) { setError(e.message); setLoading(false); } });
    const timer = setInterval(() => setNow(Date.now()), 15000);
    return () => { active = false; clearInterval(timer); };
  }, [owner]));
  async function action(kind: 'pause' | 'resume' | 'finish') {
    if (!walk || saving.current) return;
    saving.current = true; setBusy(true); setError('');
    try { setWalk(await walkStorage(owner)[kind](walk.id, Date.now())); setNow(Date.now()); setFinishing(false); }
    catch (e) { setError(e instanceof Error ? e.message : 'Не удалось сохранить действие.'); }
    finally { saving.current = false; setBusy(false); }
  }
  const next = walk?.variant.stops.find(s => walk.visits[String(s.place_id)] === undefined);
  const shape = useMemo(() => walk ? mapWalk(walk.request, walk.variant) : null, [walk]);
  const points = useMemo(() => walk?.variant.stops.map((s,i)=>({id:s.place_id,name:s.name,lat:s.location.lat,lon:s.location.lon,order:i+1,visited:walk.visits[String(s.place_id)]!==undefined})) ?? [], [walk]);
  return <SectionScreen title={walk?.status === 'finished' ? 'Итоги прогулки' : 'Моя прогулка'}>
    {loading && <Text>Восстанавливаем прогулку…</Text>}
    {error ? <><Text accessibilityRole="alert">{error}</Text><AppButton title="Повторить загрузку" onPress={() => void load()} /></> : null}
    {error && !walk && <AppButton title="Сбросить сохранённую прогулку" variant="secondary" onPress={()=>{void clearWalks(getApiBaseUrl(),owner).then(()=>load()).catch(e=>setError(e.message));}}/>}
    {!loading && !error && !walk && <><Text>Сохранённой прогулки пока нет.</Text><AppButton title="Спланировать прогулку" onPress={() => router.replace('/walks')} /></>}
    {walk && <>
      <Text>Подтверждено мест — {Object.keys(walk.visits).length} из {walk.variant.stops.length}</Text>
      <Text>{walk.request.travel_mode==='cycling'?'На велосипеде':'Пешком'}. Расписание рассчитано на {new Date(walk.request.start_at).toLocaleString('ru-RU',{timeZone:walk.timezone})}.</Text>
      <Text>Время прогулки — {duration(elapsedWalk(walk, now) / 1000)}. Плановая длина — {distance(walk.variant.totals.distance_m)}.</Text>
      {walk.status === 'paused' && <Text>Прогулка на паузе</Text>}
      {walk.status === 'active' && next && <>
        <Text>Следующая остановка — {next.name}</Text>
        <AppButton title="Открыть место" onPress={() => router.push({ pathname: '/places/[id]', params: { id: next.place_id } })} />
        <AppButton title="Подтвердить посещение" onPress={() => router.push({ pathname: '/capture/[id]', params: { id: next.place_id, walkId: walk.id } })} />
      </>}
      {!next && walk.status !== 'finished' && <Text>Все остановки подтверждены. Можно завершить прогулку.</Text>}
      {walk.status === 'active' && <AppButton title="Пауза" loading={busy} onPress={() => void action('pause')} />}
      {walk.status === 'paused' && <AppButton title="Продолжить" loading={busy} onPress={() => void action('resume')} />}
      {walk.status !== 'finished' && !finishing && <AppButton title="Завершить прогулку" disabled={busy} variant="secondary" onPress={() => setFinishing(true)} />}
      {finishing && <>
        <Text>Подтверждено {Object.keys(walk.visits).length} из {walk.variant.stops.length}. Завершить прогулку?</Text>
        <AppButton title="Завершить" loading={busy} onPress={() => void action('finish')} />
        <AppButton title="Продолжить прогулку" disabled={busy} variant="secondary" onPress={() => setFinishing(false)} />
      </>}
      <AppButton title={showMap?'Показать список':'Показать карту'} variant="secondary" onPress={()=>setShowMap(!showMap)}/>
      {showMap && shape && <View style={{height:440}}><PlacesMap token={getMapboxToken()} places={points} route={shape.route} position={null} onShowList={()=>setShowMap(false)} onSelect={id=>router.push({pathname:'/places/[id]',params:{id}})}/></View>}
      {!showMap && <WalkStopList stops={walk.variant.stops} timezone={walk.timezone} visits={walk.visits}
        onConfirm={walk.status === 'active' ? id => router.push({ pathname: '/capture/[id]', params: { id, walkId: walk.id } }) : undefined} />}
      {walk.status === 'finished' && <AppButton title="Новая прогулка" onPress={() => router.replace('/walks')} />}
    </>}
  </SectionScreen>;
}
