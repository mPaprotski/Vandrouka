import { router } from 'expo-router';
import { Text } from 'react-native';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { WalkVariantCard } from '../../components/walks/WalkVariantCard';
import { useWalkPlanner } from '../../walks/WalkProvider';

export default function WalkVariants() {
  const { calculation, draft, setDraft } = useWalkPlanner();
  const variants = calculation?.result.variants ?? [];
  return <SectionScreen title="Варианты прогулки">
    {!calculation && <><Text>Откройте параметры и постройте прогулку.</Text><AppButton title="Параметры прогулки" onPress={() => router.replace('/walks')} /></>}
    {calculation && variants.length === 0 && <>
      <Text>На это время маршрут не подобрался.</Text>
      <AppButton title="Увеличить время" onPress={() => { setDraft({ ...draft, minutes: String(Math.min(1440, Number(draft.minutes) + 30)) }); router.replace('/walks'); }} />
      <AppButton title="Изменить интересы" variant="secondary" onPress={() => router.replace('/walks')} />
    </>}
    {variants.map((variant, index) => <WalkVariantCard key={variant.variant_id} variant={variant} title={`Маршрут ${index + 1}`} onSelect={() => router.push({ pathname: '/walks/preview', params: { variantId: variant.variant_id } })} />)}
    {variants.length > 0 && <AppButton title="Изменить параметры" variant="secondary" onPress={() => router.replace('/walks')} />}
  </SectionScreen>;
}
