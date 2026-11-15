import { expect, test } from 'vitest';
import { streamUrl } from './url';

test('streamUrl is exactly include=spaces', () => {
  expect(streamUrl('http://localhost:8080/', 'L1')).toBe('http://localhost:8080/v1/lots/L1/availability/stream?include=spaces');
  expect(streamUrl('https://api.test', 'L1')).toBe('https://api.test/v1/lots/L1/availability/stream?include=spaces');
});

test('streamUrl encodes the lot id', () => {
  expect(streamUrl('http://api.test', 'a/b?x=1')).toBe(
    'http://api.test/v1/lots/a%2Fb%3Fx%3D1/availability/stream?include=spaces',
  );
});
