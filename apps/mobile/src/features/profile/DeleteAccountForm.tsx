import { deleteMeSchema } from '@mypark/contracts';
import { useTranslation } from 'react-i18next';
import { View } from 'react-native';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { FormField } from '@/features/auth/FormField';
import { useApiForm } from '@/lib/forms/use-api-form';
import { session } from '@/services/runtime';

import { deleteMe } from './api';

/**
 * The danger zone: what deletion does, the password, then the account is gone and the session ends.
 * Shared by the Profile delete screen and the legal gate, where deletion must stay available.
 */
export function DeleteAccountForm({ onDeleted }: { onDeleted(): void }) {
  const { t } = useTranslation();

  const { form, lock, serverError, onSubmit, submitting } = useApiForm({
    schema: deleteMeSchema,
    defaultValues: { password: '' },
    submit: async (values, form) => {
      // A wrong password is a 403 `auth.password_incorrect`: it lands on the field and never refreshes or ends the session.
      await deleteMe(values);
      // The password is discarded with the form before anything else happens.
      form.reset();
      // The server session died with the account: clear storage, memory and queries. The language setting stays.
      await session.signOutLocal();
      onDeleted();
    },
  });

  return (
    <View className="gap-4">
      <AppText accessibilityRole="header" weight="bold" variant="title">
        {t('profile.danger.title')}
      </AppText>
      <AppText>{t('profile.danger.body')}</AppText>
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <FormField
        control={form.control}
        name="password"
        label={t('auth.fields.password')}
        secureTextEntry
        autoComplete="current-password"
        textContentType="password"
        returnKeyType="go"
        onSubmitEditing={() => void onSubmit()}
      />
      <Button variant="danger" label={t('profile.danger.submit')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
    </View>
  );
}
