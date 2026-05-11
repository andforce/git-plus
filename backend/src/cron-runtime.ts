import { Cron } from 'croner';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { loadConfigOrDefault } from './config-store';
import type { Timestamp } from '@bufbuild/protobuf/wkt';

export type CronRuntimeSnapshot = {
  enabled: boolean;
  cron: string;
  nextRuns: Array<Timestamp>;
  lastError: string;
};

export class CronRuntime {
  #job: Cron | undefined;
  #lastError = '';

  constructor(
    private readonly dataDir: string,
    private readonly enqueueFullSync: () => void,
  ) {}

  reload(): CronRuntimeSnapshot {
    const { config } = loadConfigOrDefault(this.dataDir);
    this.apply(config.cron ?? '');
    return this.snapshot();
  }

  snapshot(): CronRuntimeSnapshot {
    const pattern = this.#job?.getPattern() ?? '';
    return {
      enabled: !!this.#job && this.#job.isRunning(),
      cron: pattern,
      nextRuns: this.#job?.nextRuns(5).map(timestampFromDate) ?? [],
      lastError: this.#lastError,
    };
  }

  close(): void {
    this.#job?.stop();
    this.#job = undefined;
  }

  private apply(pattern: string): void {
    this.#job?.stop();
    this.#job = undefined;
    this.#lastError = '';
    const trimmed = pattern.trim();
    if (!trimmed) return;
    try {
      this.#job = new Cron(
        trimmed,
        {
          protect: true,
          catch: (error) => {
            this.#lastError =
              error instanceof Error ? error.message : String(error);
          },
        },
        () => this.enqueueFullSync(),
      );
    } catch (error) {
      this.#lastError = error instanceof Error ? error.message : String(error);
    }
  }
}
