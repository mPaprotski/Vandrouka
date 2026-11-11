# Переход Expo SDK 57 → 58

Причина: установленный на физическом Android Expo Go поддерживает SDK 58,
а проект использовал SDK 57. Обновление патча 57.0.30 → 57.0.31 не меняет major SDK.

Обновлены package.json и package-lock.json: Expo 58, React Native 0.88.0,
React 19.3.0, Router 58, WebView 14.0.1, Reanimated/Worklets и Expo-модули
камеры, геолокации, SecureStore и файловой системы. Версии подобраны и проверены
`expo install --fix`; обход peer dependencies через --force/--legacy-peer-deps не применялся.
Старое дерево npm конфликтовало с Router 58: сохранено в /tmp и пересоздано.

Адаптации кода:
- начальный маршрут вкладок перенесён из удалённого initialRouteName prop в
  `unstable_settings.anchor: 'explore'` согласно новой модели Router;
- обработчик AppState истории принимает также undefined;
- необязательный FlatList footer передаёт undefined вместо null;
- WebView 14 получает явный generic object вместо undefined, чтобы строгая
  типизация сохраняла WebViewProps, а не превращала их пересечение в never.

Проверки:
- expo install --fix: Dependencies are up to date;
- expo-doctor: 20/20;
- mobile: 15 тестовых файлов, typecheck, lint;
- Android/Hermes export: /tmp/vandrouka-sdk58-export;
- Metro перезапущен с --go --lan --clear;
- Android manifest по LAN отвечает 200 и сообщает SDK 58.0.0;
- backend /health отвечает 200.

Mapbox API не вызывался, Android-эмулятор не запускался. Полный проход камеры,
GPS, карты и восстановления сессии на телефоне после обновления ещё требуется.
Автоматический npm audit сообщает пять high-записей в цепочке Expo CLI /
code-signing-certificates / node-forge / zod. Предлагаемое принудительное
исправление включает откат Expo к SDK 54 и не применялось. Это отдельная
проверка инструментальных зависимостей; не повод скрывать несовместимость SDK.

Официальные инструкции:
- https://docs.expo.dev/workflow/upgrading-expo-sdk-walkthrough/
- https://expo.dev/changelog/sdk-58
- https://docs.expo.dev/router/migrate/sdk-57-to-58/

## Исправления после проверки на телефоне

Android Camera возвращает format `.jpg`, хотя JS-тип описывает `jpg`.
Валидация теперь нормализует JPEG-формат, сохраняя проверки URI, размеров и файла.
Добавлен тест реального вида Android-ответа.

В SDK 58 глобальный fetch использует Expo multipart-кодировщик, который не
поддерживает RN-дескриптор `{uri, name, type}`. Из-за этого photo-upload
завершался до HTTP-запроса и показывал обобщённую сетевую ошибку; GET API работали.
Нативная съёмка теперь добавляет Expo File к локальному снимку, а upload передаёт
его в FormData без base64. Node-интеграционный стенд сохраняет собственный
URI-адаптер с синтетическим изображением.
Отдельный тест использует настоящие API-клиент и нативный File-адаптер
с заглушкой устройства и стандартным multipart-кодировщиком.
Повторная проверка загрузки на физическом Android остаётся обязательной.
