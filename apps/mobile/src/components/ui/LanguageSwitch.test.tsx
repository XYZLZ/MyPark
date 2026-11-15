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

const classesOf = (element: { props: { className?: string } }) => (element.props.className ?? '').split(/\s+/);

test('the selected language uses the nav-active border, the other keeps the control line', async () => {
  await render(<LanguageSwitch value="es-DO" onChange={jest.fn()} />);
  const selected = classesOf(screen.getByRole('button', { name: 'Español' }));
  const other = classesOf(screen.getByRole('button', { name: 'English' }));
  expect(selected).toContain('border-nav-active');
  expect(selected).not.toContain('border-brand-primary');
  expect(other).toContain('border-control-line');
});

test('focus is distinct from selected: a thicker navy border on both', async () => {
  await render(<LanguageSwitch value="es-DO" onChange={jest.fn()} />);
  for (const name of ['Español', 'English']) {
    const classes = classesOf(screen.getByRole('button', { name }));
    expect(classes).toEqual(expect.arrayContaining(['focus:border-4', 'focus:border-brand-navy']));
    expect(classes).not.toContain('focus:border-nav-active');
  }
});
