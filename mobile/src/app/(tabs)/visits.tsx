import { router } from 'expo-router';
import { useSession } from '../../auth/SessionProvider';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState, LoadingState } from '../../components/ui/ContentState';

export default function VisitsScreen(){
 const {state}=useSession();
 return <SectionScreen title="Ваши открытия" description="Здесь будет история ваших прогулок.">
  {state.status==='loading'?<LoadingState/>:state.status==='authenticated'?
   <EmptyState title="История посещений ещё не подключена" description="Вы вошли в аккаунт. Сохранение подтверждённых посещений появится на следующем этапе." action={<AppButton title="Исследовать город" variant="secondary" onPress={()=>router.navigate('/explore')}/>}/>:
   <EmptyState title="Нужен вход в аккаунт" description="Войдите, чтобы сохранять свои открытия. Каталог доступен без входа." action={<AppButton title="Открыть профиль" onPress={()=>router.navigate('/profile')}/>}/>
  }
 </SectionScreen>;
}
