/** An EventSource the tests drive by hand. */
export class FakeEventSource extends EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static instances: FakeEventSource[] = [];

  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;
  readonly url: string;
  readonly withCredentials: boolean;
  readyState = 0;
  onopen = null;
  onmessage = null;
  onerror = null;

  constructor(url: string | URL, init?: EventSourceInit) {
    super();
    this.url = String(url);
    this.withCredentials = init?.withCredentials ?? false;
    FakeEventSource.instances.push(this);
  }

  /** Delivers a named event with a JSON body. */
  emit(type: string, data: unknown) {
    this.readyState = 1;
    this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data) }));
  }

  /** Fails the connection the way a 401 or network error does. */
  fail(readyState = 2) {
    this.readyState = readyState;
    this.dispatchEvent(new Event("error"));
  }

  close() {
    this.readyState = 2;
  }

  static latest(): FakeEventSource {
    const es = FakeEventSource.instances.at(-1);
    if (!es) {
      throw new Error("no EventSource was opened");
    }
    return es;
  }

  static reset() {
    FakeEventSource.instances = [];
  }
}
