import { BrowserAuthProbe } from '../components/browser-auth-probe';
import { SectionScreen } from '../components/SectionScreen';

export default function AuthCheckScreen() {
  const origin = process.env.EXPO_PUBLIC_OAUTH_PROBE_URL;
  if ((!__DEV__ && process.env.EXPO_PUBLIC_ENABLE_OAUTH_PROBE !== 'true') || !origin) {
    return <SectionScreen title="Проверка недоступна" description="Проверка входа не включена для этой сборки." />;
  }
  return <BrowserAuthProbe origin={origin} />;
}
