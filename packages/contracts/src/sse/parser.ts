/** One dispatched server-sent event. `id` is the last event id, present while it is non-empty. */
export type SseFrame = { event: string; id?: string; data: string };

/** The longest line kept while waiting for its line end (1 MiB). Stream payloads are a few KiB. */
export const SSE_MAX_LINE_LENGTH = 1_048_576;

export type SseParser = {
  /** Feeds decoded text; returns the frames completed by it. Results do not depend on where chunks split. */
  push(chunk: string): SseFrame[];
  /** The last valid `retry` value in milliseconds, or `null` if none was received. */
  readonly retryMs: number | null;
  /** Lines dropped for exceeding `maxLineLength`, each with the frame it belonged to. */
  readonly droppedLines: number;
};

/**
 * Incremental parser for `text/event-stream` text, per the WHATWG "interpreting an event stream" rules.
 * Pure: no I/O and no timers. Byte decoding (UTF-8, `stream: true`) is the transport's job, and
 * transports decode with `new TextDecoder('utf-8', { ignoreBOM: true })` so this parser is the only
 * place a leading BOM is stripped (exactly one, as the spec says). Web `EventSource` parses natively
 * and does not use this module.
 *
 * A line still unterminated past `maxLineLength` is discarded with the whole frame it belongs to:
 * the fields before it, the rest of the line and every field up to the next blank line. Each such
 * line is counted in `droppedLines`.
 */
export function createSseParser({ maxLineLength = SSE_MAX_LINE_LENGTH }: { maxLineLength?: number } = {}): SseParser {
  let buffer = '';
  let pendingCr = false;
  let bomChecked = false;
  let skippingLine = false;
  let skippingFrame = false;
  let droppedLines = 0;
  let eventType = '';
  let data = '';
  let lastEventId = '';
  let retryMs: number | null = null;

  function processField(line: string, out: SseFrame[]): void {
    if (line === '') {
      if (data !== '') {
        const frame: SseFrame = { event: eventType || 'message', data: data.slice(0, -1) };
        if (lastEventId !== '') frame.id = lastEventId;
        out.push(frame);
      }
      data = '';
      eventType = '';
      return;
    }
    if (line.startsWith(':')) return;
    const colon = line.indexOf(':');
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? '' : line.slice(colon + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    switch (field) {
      case 'event':
        eventType = value;
        break;
      case 'data':
        data += `${value}\n`;
        break;
      case 'id':
        if (!value.includes('\0')) lastEventId = value;
        break;
      case 'retry':
        if (/^\d+$/.test(value)) retryMs = Number(value);
        break;
    }
  }

  return {
    push(chunk) {
      let text = chunk;
      if (!bomChecked && text !== '') {
        bomChecked = true;
        if (text.startsWith('\uFEFF')) text = text.slice(1);
      }
      if (pendingCr && text !== '') {
        pendingCr = false;
        if (text.startsWith('\n')) text = text.slice(1);
      }
      const out: SseFrame[] = [];
      // The kept buffer never holds a line end, so scanning resumes where the last chunk stopped.
      let i = buffer.length;
      buffer += text;
      let start = 0;
      for (; i < buffer.length; i++) {
        const ch = buffer[i];
        if (ch !== '\r' && ch !== '\n') continue;
        const line = buffer.slice(start, i);
        if (skippingLine) skippingLine = false;
        else if (skippingFrame) skippingFrame = line !== '';
        else processField(line, out);
        if (ch === '\r') {
          if (i + 1 === buffer.length) pendingCr = true;
          else if (buffer[i + 1] === '\n') i++;
        }
        start = i + 1;
      }
      buffer = skippingLine ? '' : buffer.slice(start);
      if (buffer.length > maxLineLength) {
        buffer = '';
        skippingLine = true;
        skippingFrame = true;
        droppedLines++;
        data = '';
        eventType = '';
      }
      return out;
    },
    get retryMs() {
      return retryMs;
    },
    get droppedLines() {
      return droppedLines;
    },
  };
}
