/** One dispatched server-sent event. `id` is the last event id, present while it is non-empty. */
export type SseFrame = { event: string; id?: string; data: string };

export type SseParser = {
  /** Feeds decoded text; returns the frames completed by it. Results do not depend on where chunks split. */
  push(chunk: string): SseFrame[];
  /** The last valid `retry` value in milliseconds, or `null` if none was received. */
  readonly retryMs: number | null;
};

/**
 * Incremental parser for `text/event-stream` text, per the WHATWG "interpreting an event stream" rules.
 * Pure: no I/O and no timers. Byte decoding (UTF-8, `stream: true`) is the transport's job.
 */
export function createSseParser(): SseParser {
  let buffer = '';
  let pendingCr = false;
  let bomChecked = false;
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
        if (text.startsWith('﻿')) text = text.slice(1);
      }
      if (pendingCr && text !== '') {
        pendingCr = false;
        if (text.startsWith('\n')) text = text.slice(1);
      }
      const out: SseFrame[] = [];
      buffer += text;
      let start = 0;
      for (let i = 0; i < buffer.length; i++) {
        const ch = buffer[i];
        if (ch !== '\r' && ch !== '\n') continue;
        processField(buffer.slice(start, i), out);
        if (ch === '\r') {
          if (i + 1 === buffer.length) pendingCr = true;
          else if (buffer[i + 1] === '\n') i++;
        }
        start = i + 1;
      }
      buffer = buffer.slice(start);
      return out;
    },
    get retryMs() {
      return retryMs;
    },
  };
}
