import { router } from 'expo-router';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState } from '../../components/ui/ContentState';

export default function ProfileScreen() {
  return (
    <SectionScreen title="Ваша история города" description="Сохраняйте открытия и следите за своим прогрессом.">
      <EmptyState
        title="Профиль ещё не подключён"
        description="Вход, опыт и уровни появятся позже. Пока можно познакомиться с местами города."
        action={<AppButton title="Исследовать город" variant="secondary" onPress={() => router.navigate('/explore')} />}
      />
    </SectionScreen>
  );
}
