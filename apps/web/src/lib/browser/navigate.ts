/**
 * A full page load to `url`: the router cache and the locale reset with the new
 * cookies. It lives in its own module so tests can replace it (jsdom cannot
 * navigate and `window.location` cannot be stubbed).
 */
export function hardNavigate(url: string): void {
  window.location.assign(url);
}
