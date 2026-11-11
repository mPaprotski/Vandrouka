import {HistoryList} from '../../components/HistoryList';
import { router } from 'expo-router';
import { useSession } from '../../auth/SessionProvider';
import { SectionScreen } from '../../components/SectionScreen';
import { AppButton } from '../../components/ui/AppButton';
import { EmptyState, LoadingState } from '../../components/ui/ContentState';

export default function VisitsScreen(){
 const {state,controller}=useSession();
 if(state.status==='authenticated')return <HistoryList key={state.user.id} owner={state.user.id} session={controller}/>;
 return <SectionScreen title="Ваши открытия" description="Здесь будет история ваших прогулок.">
  {state.status==='loading'?<LoadingState/>:   <EmptyState title="Нужен вход в аккаунт" description="Войдите, чтобы сохранять свои открытия. Каталог доступен без входа." action={<AppButton title="Открыть профиль" onPress={()=>router.navigate('/profile')}/>}/>
  }
 </SectionScreen>;
}
