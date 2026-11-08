import { fireEvent, render, screen } from '@testing-library/react-native';

import { LanguageSwitch } from './LanguageSwitch';

test('marks the selected language and names each in its own language', async () => {
  await render(<LanguageSwitch value="es-DO" onChange={jest.fn()} />);
  const es = screen.getByRole('button', { name: 'Español' });
  const en = screen.getByRole('button', { name: 'English' });
  expect(es.props.accessibilityState).toMatchObject({ selected: true });
  expect(en.props.accessibilityState).toMatchObject({ selected: false });
  expect(es.props.className).toContain('min-h-12');
  expect(en.props.className).toContain('min-h-12');
});

test('pressing a language reports it', async () => {
  const onChange = jest.fn();
  await render(<LanguageSwitch value="es-DO" onChange={onChange} />);
  await fireEvent.press(screen.getByRole('button', { name: 'English' }));
  expect(onChange).toHaveBeenCalledWith('en');
});
