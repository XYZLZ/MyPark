import { ScreenScroll } from '@/components/ui/ScreenScroll';

import { DeleteAccountForm } from './DeleteAccountForm';

/** Profile's delete screen. When the account is gone the root layout swaps to the signed-out area on its own. */
export function DeleteAccountScreen() {
  return (
    <ScreenScroll edge="tabBar" header="stack">
      <DeleteAccountForm onDeleted={() => undefined} />
    </ScreenScroll>
  );
}
