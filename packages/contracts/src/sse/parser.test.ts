import { describe, expect, test } from 'vitest';
import { availabilityReducedFixture, lotFixture, sseText } from '../fixtures';
import { createSseParser, SSE_MAX_LINE_LENGTH, type SseFrame } from './parser';

function parseAll(chunks: string[]): SseFrame[] {
  const parser = createSseParser();
  return chunks.flatMap((chunk) => parser.push(chunk));
}

const fixture =
  sseText('availability', availabilityReducedFixture, 120) + ': ping\r\n\r\n' + sseText('heartbeat', { as_of: 'x' });

describe('createSseParser', () => {
  test('parses one frame with event, id and data', () => {
    expect(parseAll(['event: availability\nid: 120\ndata: {"a":1}\n\n'])).toEqual([
      { event: 'availability', id: '120', data: '{"a":1}' },
    ]);
  });

  test('chunks split anywhere give the same frames', () => {
    const whole = parseAll([fixture]);
    expect(whole).toHaveLength(2);
    expect(parseAll([...fixture])).toEqual(whole);
    for (let i = 0; i <= fixture.length; i++) {
      expect(parseAll([fixture.slice(0, i), '', fixture.slice(i)])).toEqual(whole);
    }
  });

  test('bytes split inside a UTF-8 sequence decode to the same frames', () => {
    const { TextEncoder, TextDecoder } = globalThis as unknown as {
      TextEncoder: new () => { encode(text: string): Uint8Array };
      TextDecoder: new () => { decode(bytes?: Uint8Array, options?: { stream: boolean }): string };
    };
    const text = `data: ${lotFixture.address} 🚗\n\n`;
    const bytes = new TextEncoder().encode(text);
    const decoder = new TextDecoder();
    const parser = createSseParser();
    const frames = Array.from(bytes, (byte) => parser.push(decoder.decode(Uint8Array.of(byte), { stream: true }))).flat();
    expect(frames).toEqual([{ event: 'message', data: `${lotFixture.address} 🚗` }]);
  });

  test('CRLF and bare CR end lines; CR|LF across chunks is one line end', () => {
    expect(parseAll(['data: x\r', '\n\r\n'])).toEqual([{ event: 'message', data: 'x' }]);
    expect(parseAll(['data: x\r', '', '\n\r\n'])).toEqual([{ event: 'message', data: 'x' }]);
    expect(parseAll(['data: a\rdata: b\r\r'])).toEqual([{ event: 'message', data: 'a\nb' }]);
    expect(parseAll(['data: a\r\ndata: b\r\n\r\n'])).toEqual([{ event: 'message', data: 'a\nb' }]);
  });

  test('comment lines are ignored', () => {
    expect(parseAll([': ping\n\n'])).toEqual([]);
    expect(parseAll([':\ndata: x\n: mid\n\n'])).toEqual([{ event: 'message', data: 'x' }]);
  });

  test('retry is taken only when all digits', () => {
    const parser = createSseParser();
    expect(parser.retryMs).toBeNull();
    parser.push('retry: 3000\n');
    expect(parser.retryMs).toBe(3000);
    for (const bad of ['retry: 3s\n', 'retry:\n', 'retry: -1\n', 'retry: 1.5\n']) parser.push(bad);
    expect(parser.retryMs).toBe(3000);
    parser.push('retry:250\n');
    expect(parser.retryMs).toBe(250);
  });

  test('multi-line data joins with a newline', () => {
    expect(parseAll(['data: a\ndata: b\n\n'])).toEqual([{ event: 'message', data: 'a\nb' }]);
  });

  test('a frame without a closing blank line is not emitted', () => {
    expect(parseAll(['event: availability\ndata: {"a":1}\n'])).toEqual([]);
    expect(parseAll(['data: x'])).toEqual([]);
  });

  test('event defaults to message and empty data is not dispatched', () => {
    expect(parseAll(['event: x\n\n', 'id: 7\n\n'])).toEqual([]);
    expect(parseAll(['data: y\n\n'])).toEqual([{ event: 'message', data: 'y' }]);
    expect(parseAll(['event: a\n\ndata: y\n\n'])).toEqual([{ event: 'message', data: 'y' }]);
  });

  test('a field without a colon has an empty value; only one leading space is stripped', () => {
    expect(parseAll(['data\n\n'])).toEqual([{ event: 'message', data: '' }]);
    expect(parseAll(['data\ndata\n\n'])).toEqual([{ event: 'message', data: '\n' }]);
    expect(parseAll(['data:  two\n\n'])).toEqual([{ event: 'message', data: ' two' }]);
    expect(parseAll(['data:none\n\n'])).toEqual([{ event: 'message', data: 'none' }]);
    expect(parseAll(['event\ndata: z\n\n'])).toEqual([{ event: 'message', data: 'z' }]);
  });

  test('unknown fields are ignored', () => {
    expect(parseAll(['foo: bar\nDATA: no\ndata: y\n\n'])).toEqual([{ event: 'message', data: 'y' }]);
  });

  test('the id persists across frames, an empty id clears it and an id with NUL is ignored', () => {
    expect(parseAll(['id: 5\ndata: a\n\ndata: b\n\nid\ndata: c\n\n'])).toEqual([
      { event: 'message', id: '5', data: 'a' },
      { event: 'message', id: '5', data: 'b' },
      { event: 'message', data: 'c' },
    ]);
    expect(parseAll(['id: 5\ndata: a\n\nid: 6\0\ndata: b\n\n'])).toEqual([
      { event: 'message', id: '5', data: 'a' },
      { event: 'message', id: '5', data: 'b' },
    ]);
  });

  test('an unterminated line over the cap is dropped with its frame and reported', () => {
    expect(SSE_MAX_LINE_LENGTH).toBe(1_048_576);
    const parser = createSseParser({ maxLineLength: 16 });
    expect(parser.push('event: spaces\ndata: ok\ndata: 0123456789')).toEqual([]);
    expect(parser.push('abcdef')).toEqual([]);
    expect(parser.droppedLines).toBe(1);
    // The rest of that frame, up to the next blank line, is skipped; the next frame parses.
    expect(parser.push('xyz\ndata: tail\nid: 9\n\nevent: heartbeat\ndata: {}\n\n')).toEqual([
      { event: 'heartbeat', data: '{}' },
    ]);
    expect(parser.droppedLines).toBe(1);
  });

  test('a long line split across many chunks still parses under the cap', () => {
    const parser = createSseParser();
    const payload = 'x'.repeat(200_000);
    const text = `data: ${payload}\n\n`;
    const frames: SseFrame[] = [];
    for (let i = 0; i < text.length; i += 1_000) frames.push(...parser.push(text.slice(i, i + 1_000)));
    expect(frames).toEqual([{ event: 'message', data: payload }]);
    expect(parser.droppedLines).toBe(0);
  });

  // Transports decode with TextDecoder({ ignoreBOM: true }) so this parser is the only BOM stripper.
  test('a leading BOM is dropped once, even after an empty chunk', () => {
    expect(parseAll(['', '\uFEFFdata: a\n\n'])).toEqual([{ event: 'message', data: 'a' }]);
    expect(parseAll(['\uFEFF', '\uFEFFdata: a\n\ndata: b\n\n'])).toEqual([{ event: 'message', data: 'b' }]);
  });
});
