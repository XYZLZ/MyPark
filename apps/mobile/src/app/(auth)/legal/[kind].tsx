import { useLocalSearchParams } from 'expo-router';

import { LegalDocumentScreen } from '@/features/auth/LegalDocumentScreen';

export default function LegalRoute() {
  const { kind } = useLocalSearchParams<{ kind: string }>();
  return <LegalDocumentScreen kind={kind} />;
}
