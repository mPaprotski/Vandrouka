import {useEffect,useRef,useState} from 'react';
import {Text,TextInput} from 'react-native';
import {api} from '../../api/client';
import {SectionScreen} from '../../components/SectionScreen';
import {AppButton} from '../../components/ui/AppButton';
import {useForegroundLocation} from '../../hooks/useForegroundLocation';
import {router} from 'expo-router';
import {useWalkPlanner} from '../../walks/WalkProvider';
import type {WalkInput} from '../../walks/api';

export default function WalkPlanner(){
 const {draft,setDraft,calculation,setCalculation}=useWalkPlanner();
 const {minutes,budget,pace,categories,useGPS,distance,stops,mode}=draft;
 const setMinutes=(minutes:string)=>setDraft({...draft,minutes});
 const setBudget=(budget:string)=>setDraft({...draft,budget});
 const setPace=(pace:WalkInput['pace'])=>setDraft({...draft,pace});
 const setCategories=(categories:string[])=>setDraft({...draft,categories});
 const setUseGPS=(useGPS:boolean)=>setDraft({...draft,useGPS});
 const location=useForegroundLocation();
 const [message,setMessage]=useState('');
 const [busy,setBusy]=useState(false);
 const pending=useRef<AbortController|null>(null);
 useEffect(()=>()=>pending.current?.abort(),[]);
 async function generate(){
  if(pending.current)return;
  const duration=Number(minutes),cost=Number(budget.replace(',','.'));
  const meters=Number(distance.replace(',','.'))*1000,stopCount=Number(stops);
  if((distance!==''&&(!/^\d+([.,]\d{1,3})?$/.test(distance)||meters<=0||meters>1000000)) || (stops!==''&&(!/^\d+$/.test(stops)||stopCount<1||stopCount>100))){setMessage('Проверьте дистанцию (до 1000 км) и число мест (от 1 до 100).');return;}
  if(!/^\d+$/.test(minutes)||duration<1||duration>1440|| (budget!==''&&(!/^\d+([.,]\d{1,2})?$/.test(budget)||!Number.isSafeInteger(Math.round(cost*100))))){setMessage('Проверьте длительность (1–1440 минут) и сумму билетов.');return;}
  if(useGPS&&location.state.status!=='success'){setMessage('Получите местоположение или выберите центр Минска.');return;}
  const abort=new AbortController();pending.current=abort;setBusy(true);setMessage('Подбираем места и рассчитываем путь…');
  try{
   const cities=await api.listCities({signal:abort.signal});
   const city=cities.find(c=>c.slug==='minsk');if(!city)throw Error('Минск отсутствует в каталоге.');
   const start=useGPS&&location.state.status==='success'?{lat:location.state.fix.latitude,lon:location.state.fix.longitude}:{lat:city.center_lat,lon:city.center_lon};
   const request:WalkInput={city:'minsk',start,start_at:new Date().toISOString(),budget_minutes:duration,pace,categories,include_geometry:true,travel_mode:mode,...(distance===''?{}:{max_distance_m:meters}),...(stops===''?{}:{max_stops:stopCount}),...(budget===''?{}:{ticket_budget_minor:Math.round(cost*100)})};
   const data=await api.walks.generate(request,{signal:abort.signal});
   if(!abort.signal.aborted){
    setCalculation({request:{...request,seed:data.seed},result:data});setMessage('');
    if(data.variants.length===1)router.push({pathname:'/walks/preview',params:{variantId:data.variants[0].variant_id}});
    else router.push('/walks/variants');
   }
  }catch(error){if(!abort.signal.aborted)setMessage(error instanceof Error?error.message:'Не удалось построить маршрут.');}
  finally{if(pending.current===abort){pending.current=null;setBusy(false);}}
 }
 return <SectionScreen title="Спланировать прогулку" description="Прогулка по Минску. Начало — сейчас.">
  <AppButton title="Продолжить сохранённую прогулку" variant="secondary" onPress={()=>router.push('/walks/active')}/>
  <AppButton title={mode==='walking'?'Пешком ✓':'Пешком'} disabled={busy} onPress={()=>setDraft({...draft,mode:'walking'})}/>
  <AppButton title={mode==='cycling'?'На велосипеде ✓':'На велосипеде'} disabled={busy} onPress={()=>setDraft({...draft,mode:'cycling'})}/>
  <Text>Максимальная дистанция в км. Пустое поле — без лимита</Text><TextInput accessibilityLabel="Максимальная дистанция" value={distance} onChangeText={distance=>setDraft({...draft,distance})} keyboardType="decimal-pad" editable={!busy}/>
  <Text>Не больше мест. Пустое поле — без лимита</Text><TextInput accessibilityLabel="Максимальное число мест" value={stops} onChangeText={stops=>setDraft({...draft,stops})} keyboardType="number-pad" editable={!busy}/>
  <AppButton title={useGPS?'Начать от центра Минска':'Начать от моего положения'} disabled={busy} onPress={()=>{setUseGPS(!useGPS);if(!useGPS)void location.locate();}}/>
  {useGPS&&<Text>{location.state.status==='success'?'Местоположение получено':location.state.status==='error'?location.state.message:'Определяем местоположение…'}</Text>}
  <Text>Длительность в минутах</Text><TextInput accessibilityLabel="Длительность в минутах" value={minutes} onChangeText={setMinutes} keyboardType="number-pad" editable={!busy}/>
  {mode==='walking'&&(['slow','normal','fast'] as const).map((p,i)=><AppButton key={p} title={`${pace===p?'✓ ':''}${['Спокойный','Обычный','Быстрый'][i]}`} disabled={busy} onPress={()=>setPace(p)}/>)}
  <Text>Интересы. Пустой выбор — все категории</Text>
  {['museums','parks','architecture','religious','monuments','streets'].map((c,i)=><AppButton key={c} title={`${categories.includes(c)?'✓ ':''}${['Музеи','Парки','Архитектура','Религиозные места','Памятники','Улицы'][i]}`} disabled={busy} onPress={()=>setCategories(categories.includes(c)?categories.filter(x=>x!==c):[...categories,c])}/>)}
  <Text>Бюджет билетов в BYN, пустое поле — без лимита</Text><TextInput accessibilityLabel="Бюджет билетов" value={budget} onChangeText={setBudget} keyboardType="decimal-pad" editable={!busy}/>
  <AppButton title="Построить маршруты" disabled={busy} onPress={()=>void generate()}/>
  {busy&&<AppButton title="Отменить" onPress={()=>{pending.current?.abort();pending.current=null;setBusy(false);setMessage('Расчёт отменён.');}}/>}
  <Text accessibilityLiveRegion="polite">{message}</Text>
  {calculation&&<AppButton title="Вернуться к последнему расчёту" variant="secondary" disabled={busy} onPress={()=>router.push('/walks/variants')}/>}
 </SectionScreen>;
}
