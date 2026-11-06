import type { ReactNode } from 'react';

// Driver pages (dashboard, reserve, reservations, vehicles, payments, profile).
// The session requirement is added in M1.
export default function DriverLayout({ children }: { children: ReactNode }) {
  return <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-8">{children}</main>;
}
