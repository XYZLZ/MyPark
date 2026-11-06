import type { ReactNode } from 'react';

// Public pages: home (availability), login, register, verify, reset.
export default function PublicLayout({ children }: { children: ReactNode }) {
  return <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-8">{children}</main>;
}
