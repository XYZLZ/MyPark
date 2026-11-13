import { resetSchema } from '@mypark/contracts';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { AppText } from '@/components/ui/AppText';
import { Banner } from '@/components/ui/Banner';
import { Button } from '@/components/ui/Button';
import { ScreenScroll } from '@/components/ui/ScreenScroll';
import { useApiForm } from '@/lib/forms/use-api-form';
import { session } from '@/services/runtime';
import { useAuthFlowStore } from '@/store/auth-flow';

import { resetPassword } from './api';
import { FormField, stripWhitespace } from './FormField';

/** The route's header carries the title, so this screen only has the intro. */
export function ResetPasswordScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const savedEmail = useAuthFlowStore((s) => s.email);
  const setEmail = useAuthFlowStore((s) => s.setEmail);

  const { form, lock, serverError, onSubmit, submitting } = useApiForm({
    schema: resetSchema,
    defaultValues: { email: savedEmail, code: '', new_password: '' },
    submit: async (values, form) => {
      await resetPassword(values);
      // The reset revoked every session on the server, so end this device's one too (spec 5.8).
      await session.signOutLocal();
      setEmail(values.email);
      form.reset();
      router.replace('/login');
    },
  });
  const { control, setFocus } = form;

  return (
    <ScreenScroll>
      <AppText>{t('auth.reset.intro')}</AppText>
      {serverError ? <Banner tone="error">{serverError}</Banner> : null}
      <FormField
        control={control}
        name="email"
        label={t('auth.fields.email')}
        keyboardType="email-address"
        autoCapitalize="none"
        autoComplete="email"
        textContentType="emailAddress"
        spellCheck={false}
        returnKeyType="next"
        onSubmitEditing={() => setFocus('code')}
      />
      <FormField
        control={control}
        name="code"
        label={t('auth.fields.code')}
        keyboardType="number-pad"
        autoComplete="one-time-code"
        textContentType="oneTimeCode"
        spellCheck={false}
        clean={stripWhitespace}
        returnKeyType="next"
        onSubmitEditing={() => setFocus('new_password')}
      />
      <FormField
        control={control}
        name="new_password"
        label={t('auth.fields.newPassword')}
        secureTextEntry
        autoComplete="new-password"
        textContentType="newPassword"
        returnKeyType="go"
        onSubmitEditing={() => void onSubmit()}
      />
      <Button variant="primary" label={t('auth.reset.submit')} loading={submitting} disabled={lock.locked} onPress={() => void onSubmit()} />
    </ScreenScroll>
  );
}
