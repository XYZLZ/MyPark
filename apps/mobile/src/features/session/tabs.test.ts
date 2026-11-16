import { TAB_ORDER } from './tabs';

test('tabs are Inicio, Vehículos, Perfil in that order', () => {
  expect(TAB_ORDER).toEqual(['home', 'vehicles', 'profile']);
});
