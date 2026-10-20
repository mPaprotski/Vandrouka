/** Local USB development default. Public: Expo embeds this value in the bundle. */
export const DEFAULT_API_URL = 'http://127.0.0.1:8080';

export class ApiConfigurationError extends Error {
  constructor() {
    // Do not echo the supplied URL: it could accidentally contain credentials.
    super('Укажите EXPO_PUBLIC_API_URL как адрес сервера http(s)://host:port без пути, пароля и параметров.');
    this.name = 'ApiConfigurationError';
  }
}

export function parseApiBaseUrl(value: string | undefined): string {
  const input = value === undefined ? DEFAULT_API_URL : value.trim();
  let url: URL;
  try {
    url = new URL(input);
  } catch {
    throw new ApiConfigurationError();
  }
  if (
    !['http:', 'https:'].includes(url.protocol) || !url.hostname ||
    url.username || url.password || url.pathname !== '/' ||
    url.search || url.hash || input.includes('?') || input.includes('#')
  ) {
    throw new ApiConfigurationError();
  }
  return url.origin;
}

/** Keep dot notation: Expo CLI statically replaces this exact expression. */
export function getApiBaseUrl(): string {
  return parseApiBaseUrl(process.env.EXPO_PUBLIC_API_URL);
}
