/**
 * Response headers applied to every route (CC-20). The CSP here is limited to
 * the directives that need no per-request nonce: clickjacking, base-tag and
 * form-target hijacking, and plugin content.
 */
export const securityHeaders: { key: string; value: string }[] = [
  { key: 'X-Content-Type-Options', value: 'nosniff' },
  { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
  {
    key: 'Content-Security-Policy',
    value: "frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'",
  },
];
