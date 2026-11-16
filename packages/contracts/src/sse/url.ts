/** The availability stream URL with exactly `include=spaces` and nothing else (M2B-9). */
export function streamUrl(origin: string, lotId: string): string {
  return `${origin.replace(/\/+$/, '')}/v1/lots/${encodeURIComponent(lotId)}/availability/stream?include=spaces`;
}
