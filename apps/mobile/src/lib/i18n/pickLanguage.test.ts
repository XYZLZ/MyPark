import { pickLanguage } from './pickLanguage';

test('pickLanguage falls back to es-DO', () => {
  expect(pickLanguage(['fr-FR'])).toBe('es-DO');
  expect(pickLanguage([])).toBe('es-DO');
});

test('pickLanguage maps es-MX to es-DO and en-US to en', () => {
  expect(pickLanguage(['es-MX'])).toBe('es-DO');
  expect(pickLanguage(['en-US'])).toBe('en');
});

test('pickLanguage uses the first supported tag in device order', () => {
  expect(pickLanguage(['fr-FR', 'en-GB', 'es-DO'])).toBe('en');
  expect(pickLanguage(['ES-419', 'en-US'])).toBe('es-DO');
  expect(pickLanguage(['en'])).toBe('en');
});
