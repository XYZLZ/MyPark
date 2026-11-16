import { describe, expect, test } from 'vitest';
import { readPublicApiOrigin } from './public-api';

describe('readPublicApiOrigin', () => {
  test('readPublicApiOrigin keeps only the origin', () => {
    expect(readPublicApiOrigin('http://localhost:8080')).toBe('http://localhost:8080');
    expect(readPublicApiOrigin('https://api.example.do/base/?x=1')).toBe('https://api.example.do');
    expect(readPublicApiOrigin('https://api.example.do:443/#frag')).toBe('https://api.example.do');
  });

  test('readPublicApiOrigin rejects non-http and credentialed values', () => {
    for (const value of [
      'javascript:alert(1)',
      'ftp://api.local',
      '',
      undefined,
      'localhost:8080',
      'http://user:pw@api:8080',
      'http://user@api:8080',
      'not a url',
    ]) {
      expect(readPublicApiOrigin(value), String(value)).toBeNull();
    }
  });
});
