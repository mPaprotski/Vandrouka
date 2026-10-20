import type { PositionFix } from '../location/locate';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, Linking, StyleSheet, Text, View } from 'react-native';
import { WebView } from 'react-native-webview';
import { MAP_DOCUMENT_ORIGIN, isMapDocumentNavigation, mapDocument, parseMapMessage, placesScript, userLocationScript, type MapPlace, type MapStage } from '../map/document';
import { AppButton } from './ui/AppButton';
import { EmptyState, ErrorState, LoadingState } from './ui/ContentState';
import { colors } from './ui/theme';

export function PlacesMap({ token, places, position, onSelect, onShowList }: {
  token: string | null; places: readonly MapPlace[]; position: PositionFix | null; onSelect: (id: number) => void; onShowList: () => void;
}) {
  const webView = useRef<WebView>(null);
  const stage = useRef<MapStage | 'webview' | 'ready'>('webview');
  const [diagnostic, setDiagnostic] = useState<string>();
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [tileCode, setTileCode] = useState('MAP_TILES');
  const [tiles, setTiles] = useState<'loading' | 'ready' | 'slow'>('loading');
  const [phase, setPhase] = useState<'loading' | 'ready' | 'error'>('loading');
  const [message, setMessage] = useState('Не удалось загрузить карту. Проверьте интернет или откройте список мест.');
  const [attempt, setAttempt] = useState(0);
  const html = useMemo(() => token ? mapDocument(token) : null, [token]);
  const source = useMemo(() => ({ html: html ?? '', baseUrl: MAP_DOCUMENT_ORIGIN }), [html]);
  function stopTimer() { if (timer.current) clearTimeout(timer.current); timer.current = null; }
  function fail(text: string) { stopTimer(); setMessage(text); setPhase('error'); }

  useEffect(() => {
    if (!html) return;
    stage.current = 'webview';
    timer.current = setTimeout(() => {
      if (stage.current === 'ready') return;
      const labels = { webview: 'Не удалось запустить окно карты.', document: 'Не удалось дождаться загрузки библиотеки карты.', library: 'Не удалось дождаться запуска карты.', style: 'Не удалось дождаться загрузки стиля карты.', sources: 'Стиль получен, но его ресурсы ещё не загрузились.' };
      setDiagnostic('MAP_' + stage.current.toUpperCase());
      setMessage(labels[stage.current] + ' Повторите попытку или откройте список мест.');
      setPhase('error');
    }, 30000);
    return stopTimer;
  }, [html, attempt]);

  useEffect(() => {
    if (phase === 'ready') webView.current?.injectJavaScript(placesScript(places));
  }, [places, phase]);

  useEffect(() => {
    if (phase === 'ready') webView.current?.injectJavaScript(userLocationScript(position));
  }, [position, phase]);

  function openAttribution(url: string) {
    try {
      const parsed = new URL(url);
      if (parsed.protocol === 'https:' && ['mapbox.com', 'www.mapbox.com', 'apps.mapbox.com', 'www.openstreetmap.org', 'openstreetmap.org'].includes(parsed.hostname)) {
        void Linking.openURL(url).catch(() => {});
      }
    } catch { /* Block unexpected navigation. */ }
  }

  if (!html) return (
    <View style={styles.fallback}>
      <EmptyState title="Карта пока недоступна" description="Пока можно выбрать место из списка." action={<AppButton title="Открыть список" onPress={onShowList} />} />
    </View>
  );

  return (
    <View style={styles.container}>
      <WebView
        key={attempt}
        ref={webView}
        source={source}
        onLoadEnd={() => webView.current?.injectJavaScript('window.reportMapStatus && window.reportMapStatus(); true;')}
        onLayout={() => webView.current?.injectJavaScript('window.resizeMap && window.resizeMap(); true;')}
        originWhitelist={['*']}
        androidLayerType="hardware"
        javaScriptEnabled
        geolocationEnabled={false}
        domStorageEnabled
        scrollEnabled={false}
        mixedContentMode="never"
        allowFileAccess={false}
        javaScriptCanOpenWindowsAutomatically={false}
        onShouldStartLoadWithRequest={request => {
          if (isMapDocumentNavigation(request.url)) return true;
          openAttribution(request.url); return false;
        }}
        onOpenWindow={event => openAttribution(event.nativeEvent.targetUrl)}
        onMessage={event => {
          const value = parseMapMessage(event.nativeEvent.data, places);
          if (!value) return;
          if (value.type === 'progress') { stage.current = value.stage; return; }
          if (value.type === 'tile-status') {
            setTileCode(!value.rendered ? 'MAP_RENDER' : !value.sourceRequested ? 'MAP_SOURCE_START' : !value.sourceLoaded ? 'MAP_SOURCE_PENDING' : 'MAP_TILE_PENDING'); return;
          }
          if (value.type === 'tiles') {
            stage.current = 'ready'; stopTimer(); setTiles('ready'); setDiagnostic(undefined); setPhase('ready');
            webView.current?.injectJavaScript(placesScript(places));
            webView.current?.injectJavaScript(userLocationScript(position)); return;
          }
          if (value.type === 'ready') {
            stage.current = 'ready'; stopTimer(); setDiagnostic(undefined); setPhase('ready');
            webView.current?.injectJavaScript(placesScript(places));
            webView.current?.injectJavaScript(userLocationScript(position));
            setTiles('loading');
            timer.current = setTimeout(() => {
              setTiles('slow');
              webView.current?.injectJavaScript('window.reportTileStatus && window.reportTileStatus(); true;');
            }, 30000);
          } else if (value.type === 'select') onSelect(value.id);
          else { setDiagnostic('MAP_' + value.code.toUpperCase()); fail(value.code === 'webgl' ? 'Устройство не смогло отобразить карту. Используйте список мест.' : value.code === 'auth' ? 'Карта временно недоступна. Используйте список мест.' : 'Не удалось загрузить карту. Проверьте интернет и повторите.'); }
        }}
        onError={() => fail('Не удалось открыть карту. Проверьте интернет и повторите.')}
        onRenderProcessGone={() => fail('Карта была остановлена системой. Попробуйте открыть её снова.')}
        onContentProcessDidTerminate={() => fail('Карта была остановлена системой. Попробуйте открыть её снова.')}
        style={styles.webview}
      />
      {phase === 'ready' && tiles !== 'ready' && <View style={styles.tileNotice} accessibilityLiveRegion="polite">
        {tiles === 'loading' && <ActivityIndicator color={colors.primary} />}
        <Text style={styles.noticeText}>{tiles === 'slow' ? `Подложка карты не загрузилась. Можно выбрать маркер или открыть список. Код: ${tileCode}` : 'Загружаем подложку карты…'}</Text>
      </View>}
      {phase !== 'ready' && <View pointerEvents={phase === 'loading' ? 'box-none' : 'auto'} style={[styles.overlay, phase === 'loading' && styles.loadingOverlay]}>
        {phase === 'loading'
          ? <LoadingState message="Загружаем карту Минска…" />
          : <ErrorState title="Карта недоступна" message={message} requestId={diagnostic} onRetry={() => { setDiagnostic(undefined); setPhase('loading'); setAttempt(value => value + 1); }} />}
        <AppButton title="Открыть список" variant="secondary" onPress={onShowList} />
      </View>}
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.background },
  webview: { flex: 1, backgroundColor: colors.background },
  fallback: { padding: 22 },
  tileNotice: { position: 'absolute', top: 12, left: 12, right: 60, borderRadius: 12, backgroundColor: colors.surface, padding: 12, flexDirection: 'row', gap: 10 },
  noticeText: { flex: 1, color: colors.secondary, fontSize: 13 },
  loadingOverlay: { backgroundColor: 'transparent', justifyContent: 'flex-end' },
  overlay: { position: 'absolute', top: 0, bottom: 0, left: 0, right: 0, backgroundColor: colors.background, padding: 22, justifyContent: 'center', gap: 14 },
});
