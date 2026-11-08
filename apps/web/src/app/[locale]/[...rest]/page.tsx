import { notFound } from 'next/navigation';

// Unmatched URLs under a valid locale reach the localized not-found page.
export default function CatchAllPage() {
  notFound();
}
