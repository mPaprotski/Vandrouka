import {useSession} from '../../auth/SessionProvider';
import type {SessionController} from '../../auth/session';
import {useVisitedPlaces} from '../../hooks/useVisitedPlaces';
import {filterVisited,type VisitFilter} from '../../exploration/visited';
import { router } from 'expo-router';
import { useCallback, useState } from 'react';
import { FlatList, Linking, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { api } from '../../api/client';
import { PlacesMap } from '../../components/PlacesMap';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState, ErrorState, LoadingState } from '../../components/ui/ContentState';
import { PlaceCard } from '../../components/ui/PlaceCard';
import { colors } from '../../components/ui/theme';
import { useForegroundLocation } from '../../hooks/useForegroundLocation';
import { useApiResource } from '../../hooks/useApiResource';
import { demoPlaces } from '../../places/presentation';
import { getMapboxToken } from '../../map/document';

export default function ExploreScreen(){
 const {state,controller}=useSession();const owner=state.status==='authenticated'?state.user.id:null;
 return <ExploreContent key={owner??'public'} owner={owner} session={controller}/>;
}
function ExploreContent({owner,session}:{owner:number|null;session:SessionController}) {
  const visits=useVisitedPlaces(owner,session);
  const [visitFilter,setVisitFilter]=useState<VisitFilter>('all');
  const visitedIds=visits.state.status==='ready'?visits.state.ids:null;
  const [onlyDemo, setOnlyDemo] = useState(true);
  const [mode, setMode] = useState<'map' | 'list'>('map');
  const load = useCallback(async (signal: AbortSignal) => {
    const [places, categories] = await Promise.all([
      api.listPlaces('minsk', {}, { signal }), api.listCategories('minsk', { signal }),
    ]);
    return { places, categories };
  }, []);
  const { state, retry, data: catalog } = useApiResource(load);
  const select = useCallback((id: number) => router.push({ pathname: '/places/[id]', params: { id: String(id) } }), []);
  const curated = demoPlaces(catalog?.places ?? []);
  const basePlaces = onlyDemo && curated.length > 0 ? curated : catalog?.places ?? [];
  const visiblePlaces = filterVisited(basePlaces,visitedIds,visitedIds===null?'all':visitFilter).map(place=>({...place,...(visitedIds===null?{}:{visited:visitedIds.has(place.id)})}));
  const token = getMapboxToken();
  const location = useForegroundLocation();
  const [settingsError, setSettingsError] = useState<string>();

  return (
    <SafeAreaView edges={['left', 'right']} style={styles.screen}>
      <View style={styles.toolbar}>
        <Text accessibilityRole="header" style={styles.title}>МИНСК</Text>
        <View style={styles.modes}>
          <View style={styles.mode}><AppButton title="Карта" variant={mode === 'map' ? 'primary' : 'secondary'} onPress={() => setMode('map')} /></View>
          <View style={styles.mode}><AppButton title="Список" variant={mode === 'list' ? 'primary' : 'secondary'} onPress={() => setMode('list')} /></View>
        </View>
        {curated.length > 0 && <AppButton title={onlyDemo ? `Первая прогулка · ${curated.length} мест · Показать все` : `Все места · ${catalog?.places.length ?? 0} · Открыть подборку`} variant="secondary" onPress={() => setOnlyDemo(value => !value)} />}
        {owner!==null?<>
          <Text style={styles.locationMessage}>{visits.state.status==='ready'?`Посещено ${catalog?.places.filter(place=>visits.state.status==='ready'&&visits.state.ids.has(place.id)).length??0} из ${catalog?.places.length??0} мест каталога. ✓ Посещено · ○ Ещё не посещено` : visits.state.status==='error'?visits.state.message:'Статус посещений пока неизвестен. Загружаем историю…'}</Text>
          {visits.state.status==='ready'&&<View style={styles.modes}>{([['all','Все'],['visited','Мои места'],['unvisited','Новые']] as const).map(([value,label])=><View key={value} style={styles.mode}><AppButton title={label} variant={visitFilter===value?'primary':'secondary'} onPress={()=>{setVisitFilter(value);if(value!=='all')setOnlyDemo(false);}}/></View>)}</View>}
          <AppButton title="Обновить посещения" variant="secondary" loading={visits.state.status==='loading'} onPress={visits.retry}/>
        </>:<AppButton title="Войти для личной карты" variant="secondary" onPress={()=>router.navigate('/profile')}/>}
        {mode === 'map' && <AppButton title="Моё местоположение" variant="secondary" disabled={!token || !catalog} loading={location.state.status === 'loading'} onPress={() => { setSettingsError(undefined); void location.locate(); }} />}
        {location.state.status === 'error' && <Text accessibilityRole="alert" style={styles.locationMessage}>{location.state.message}</Text>}
        {location.state.status === 'error' && location.state.blocked && <AppButton title="Открыть настройки" variant="secondary" onPress={() => { void Linking.openSettings().catch(() => setSettingsError('Не удалось открыть настройки. Разрешите геолокацию в настройках телефона.')); }} />}
        {settingsError && <Text accessibilityRole="alert" style={styles.locationMessage}>{settingsError}</Text>}
        {location.state.status === 'success' && <Text style={styles.locationMessage}>Положение на момент запроса. {location.state.fix.accuracy === null ? 'Точность неизвестна.' : `Точность около ${Math.ceil(location.state.fix.accuracy)} м.`}</Text>}
      </View>
      {state.status === 'error' && <View style={styles.notice}><ErrorState message={state.message} requestId={state.requestId} onRetry={retry} /></View>}
      {!catalog && state.status === 'loading' && <View style={styles.notice}><LoadingState /></View>}
      {catalog && catalog.places.length === 0 && <View style={styles.notice}><EmptyState title="Мест пока нет" description="В этом городе пока нет доступных мест для прогулки." /></View>}
      {catalog && catalog.places.length > 0 && <>
        {state.status !== 'success' && <Text style={styles.hint}>Показаны ранее загруженные места.</Text>}
        {visiblePlaces.length===0&&<View style={styles.notice}><EmptyState title="По фильтру мест нет" description="Выберите другой фильтр или покажите все места вместо подборки."/></View>}
        <View style={[styles.map, mode !== 'map' && styles.hidden]}>
          <PlacesMap position={location.state.status === 'success' ? location.state.fix : null} token={token} places={visiblePlaces} onSelect={select} onShowList={() => setMode('list')} />
        </View>
        {mode === 'list' && <FlatList
          data={visiblePlaces}
          keyExtractor={place => String(place.id)}
          contentContainerStyle={styles.list}
          renderItem={({ item: place }) => <PlaceCard title={place.name} description={place.summary} cover={place.content?.cover} visited={place.visited}
            categories={place.categories.map(slug => catalog.categories.find(category => category.slug === slug)?.name ?? slug)}
            onPress={() => select(place.id)} />}
        />}
      </>}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.background },
  toolbar: { padding: 16, gap: 12 },
  title: { color: colors.accent, letterSpacing: 2, fontSize: 12 },
  modes: { flexDirection: 'row', gap: 12 },
  mode: { flex: 1 },
  map: { flex: 1 },
  hidden: { display: 'none' },
  list: { padding: 20, gap: 14 },
  notice: { padding: 16 },
  locationMessage: { color: colors.secondary, fontSize: 13, lineHeight: 19 },
  hint: { color: colors.secondary, paddingHorizontal: 16, paddingBottom: 8, fontSize: 12 },
});
