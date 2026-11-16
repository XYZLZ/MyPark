import 'server-only';

/**
 * The API's public origin for the browser's live stream (M2B-16). Read at
 * request time in Server Components and passed down as a prop, never inlined
 * into the bundle. Returns null unless the value is an absolute http(s) URL
 * without credentials; callers then skip the live connection.
 */
export function readPublicApiOrigin(value: string | undefined = process.env.API_PUBLIC_URL): string | null {
  if (!value) return null;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return null;
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
  if (url.username || url.password) return null;
  return url.origin;
}
