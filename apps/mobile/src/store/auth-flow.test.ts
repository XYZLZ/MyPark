import { useAuthFlowStore } from './auth-flow';

beforeEach(() => useAuthFlowStore.setState(useAuthFlowStore.getInitialState(), true));

test('carries only the email and clears it', () => {
  useAuthFlowStore.getState().setEmail('ana@example.do');
  expect(useAuthFlowStore.getState().email).toBe('ana@example.do');
  expect(Object.keys(useAuthFlowStore.getState()).sort()).toEqual(['clear', 'email', 'setEmail']);

  useAuthFlowStore.getState().clear();
  expect(useAuthFlowStore.getState().email).toBe('');
});
