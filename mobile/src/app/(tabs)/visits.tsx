import { router } from 'expo-router';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState } from '../../components/ui/ContentState';

export default function VisitsScreen() {
  return (
    <SectionScreen title="Ваши открытия" description="Здесь будет история ваших прогулок.">
      <EmptyState
        title="Посещений пока нет"
        description="Сохранение посещений пока недоступно. Здесь появятся открытые места и фотографии прогулок."
        action={<AppButton title="Исследовать город" variant="secondary" onPress={() => router.navigate('/explore')} />}
      />
    </SectionScreen>
  );
}
