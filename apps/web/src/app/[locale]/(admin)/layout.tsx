import type { ReactNode } from 'react';

// Admin pages live under (admin)/admin/. The server-side role check is added in M1.
export default function AdminLayout({ children }: { children: ReactNode }) {
  return <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-8">{children}</main>;
}
