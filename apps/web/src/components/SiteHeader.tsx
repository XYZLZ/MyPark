/** Presentational: the brand bar. The container passes the localized app name. */
export function SiteHeader({ appName }: { appName: string }) {
  return (
    <header className="bg-brand-primary text-brand-primary-contrast">
      <div className="mx-auto flex max-w-5xl items-center gap-3 px-4 py-4">
        <span aria-hidden="true" className="h-6 w-1.5 rounded-full bg-brand-accent" />
        <p className="text-lg font-semibold tracking-tight">{appName}</p>
      </div>
    </header>
  );
}
