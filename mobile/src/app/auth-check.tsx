import { BrowserAuthProbe } from '../components/browser-auth-probe';
import { SectionScreen } from '../components/SectionScreen';

export default function AuthCheckScreen() {
  const origin = process.env.EXPO_PUBLIC_OAUTH_PROBE_URL;
  if (!__DEV__ || !origin) {
    return <SectionScreen title="Проверка недоступна" description="Этот экран доступен при разработке с настроенным сервером проверки." />;
  }
  return <BrowserAuthProbe origin={origin} />;
}
