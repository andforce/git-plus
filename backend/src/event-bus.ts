import type { JsonRecord } from './types';

export type BusEvent = {
  name: string;
  channel: string;
  event: JsonRecord;
};

type Listener = (event: BusEvent) => void;

export class EventBus {
  #listeners = new Set<Listener>();

  publish(event: BusEvent): void {
    for (const listener of this.#listeners) {
      listener(event);
    }
  }

  subscribe(channel: string, signal?: AbortSignal): AsyncIterable<JsonRecord> {
    const queue: Array<JsonRecord> = [];
    let notify: (() => void) | undefined;

    const listener: Listener = (event) => {
      if (event.channel !== channel) return;
      queue.push(event.event);
      notify?.();
    };
    this.#listeners.add(listener);

    const cleanup = () => {
      this.#listeners.delete(listener);
      notify?.();
    };
    signal?.addEventListener('abort', cleanup, { once: true });

    return {
      [Symbol.asyncIterator]: async function* () {
        try {
          while (!signal?.aborted) {
            if (queue.length === 0) {
              await new Promise<void>((resolve) => {
                notify = resolve;
              });
              notify = undefined;
            }
            const next = queue.shift();
            if (next) yield next;
          }
        } finally {
          cleanup();
        }
      },
    };
  }
}
