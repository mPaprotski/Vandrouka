import { router, useLocalSearchParams } from 'expo-router';
import { useCallback } from 'react';
import { Text } from 'react-native';
import { api } from '../../api/client';
import { PlaceInformation } from '../../components/PlaceInformation';
import { SectionScreen, sectionStyles } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState, ErrorState, LoadingState } from '../../components/ui/ContentState';
import { useApiResource } from '../../hooks/useApiResource';

export default function PlaceDetailsScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const load = useCallback(async (signal: AbortSignal) => {
    const place = await api.getPlace(id ?? '', { signal });
    const [categories, cities] = await Promise.all([api.listCategories(place.city, { signal }), api.listCities({ signal })]);
    return { place, categories, city: cities.find(city => city.slug === place.city) };
  }, [id]);
  const { state, retry } = useApiResource(load);

  return (
    <SectionScreen
      title={state.status === 'success' ? state.data.place.name : 'Место'}
      description={state.status === 'success' ? state.data.place.summary || 'Описание пока не добавлено.' : 'Узнайте больше о месте перед прогулкой.'}
    >
      {state.status === 'loading' && <LoadingState message="Загружаем место…" />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={retry} />}
      {state.status === 'success' && (
        state.data.place.categories.length > 0
          ? <Text style={sectionStyles.note}>{state.data.place.categories.map(slug => state.data.categories.find(category => category.slug === slug)?.name ?? slug).join(' · ')}</Text>
          : <EmptyState title="Категории не указаны" description="Категории этого места пока не добавлены." />
      )}
      {state.status === 'success' && <PlaceInformation key={state.data.place.id} place={state.data.place} city={state.data.city} />}
      <AppButton title="К исследованию города" variant="secondary" onPress={() => router.replace('/explore')} />
    </SectionScreen>
  );
}
