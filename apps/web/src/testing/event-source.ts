/**
 * Hand-driven `EventSource` double (M2B-41). Each `new FakeEventSource(url)` is recorded in
 * `instances`; tests drive it with `open()`, `emit()` and `fail()`. A closed instance ignores
 * further input, as a real `EventSource` dispatches nothing after `close()`.
 */
export class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  readonly withCredentials: boolean;
  readyState: 0 | 1 | 2 = 0;
  closed = false;

  private readonly events = new EventTarget();
  // EventTarget implements both; the casts only add EventSource's typed overloads.
  readonly addEventListener = this.events.addEventListener.bind(this.events) as EventSource['addEventListener'];
  readonly removeEventListener = this.events.removeEventListener.bind(this.events) as EventSource['removeEventListener'];

  constructor(url: string, init?: EventSourceInit) {
    this.url = url;
    this.withCredentials = init?.withCredentials ?? false;
    FakeEventSource.instances.push(this);
  }

  close(): void {
    this.closed = true;
    this.readyState = 2;
  }

  open(): void {
    if (this.closed) return;
    this.readyState = 1;
    this.events.dispatchEvent(new Event('open'));
  }

  /** Dispatches a named event; non-string `data` is sent as JSON text. */
  emit(event: string, data: unknown, id?: string | number): void {
    if (this.closed) return;
    const text = typeof data === 'string' ? data : JSON.stringify(data);
    this.events.dispatchEvent(new MessageEvent(event, { data: text, lastEventId: id === undefined ? '' : String(id) }));
  }

  /** `0`: a network drop (the browser would retry). `2`: an HTTP failure such as 429 or 404 (it would not). */
  fail(readyState: 0 | 2): void {
    if (this.closed) return;
    this.readyState = readyState;
    this.events.dispatchEvent(new Event('error'));
  }

  /** The most recent instance; throws when none was created. */
  static latest(): FakeEventSource {
    const last = FakeEventSource.instances.at(-1);
    if (!last) throw new Error('no EventSource was created');
    return last;
  }
}
