import assert from 'node:assert/strict';
import test from 'node:test';
import { ApiConfigurationError, getApiBaseUrl, parseApiBaseUrl } from '../src/config/api.ts';

test('USB default and explicit LAN/HTTPS/IPv6 origins', () => {
  assert.equal(parseApiBaseUrl(undefined), 'http://127.0.0.1:8080');
  assert.equal(parseApiBaseUrl('  http://192.168.1.20:8080/  '), 'http://192.168.1.20:8080');
  assert.equal(parseApiBaseUrl('https://api.example.com/'), 'https://api.example.com');
  assert.equal(parseApiBaseUrl('http://[::1]:8080'), 'http://[::1]:8080');
});

test('rejects invalid origins and URLs with secrets, paths or parameters', () => {
  for (const value of ['', '  ', '192.168.1.20:8080', 'ftp://host', 'file:///tmp/data',
    'http://', 'http://host:99999', 'https://user:secret@host',
    'http://host/api', 'http://host?token=secret', 'http://host#secret', 'http://host?',
  ]) {
    assert.throws(() => parseApiBaseUrl(value), ApiConfigurationError);
  }
});

test('reads EXPO_PUBLIC_API_URL and does not expose bad input in errors', () => {
  const previous = process.env.EXPO_PUBLIC_API_URL;
  try {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.com';
    assert.equal(getApiBaseUrl(), 'https://api.example.com');
    process.env.EXPO_PUBLIC_API_URL = 'https://user:private-password@example.com';
    assert.throws(getApiBaseUrl, error => error instanceof ApiConfigurationError && !error.message.includes('private-password'));
  } finally {
    if (previous === undefined) delete process.env.EXPO_PUBLIC_API_URL;
    else process.env.EXPO_PUBLIC_API_URL = previous;
  }
});
