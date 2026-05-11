import { randomUUID } from 'node:crypto';
import { Code, ConnectError } from '@connectrpc/connect';
import {
  TaskEnqueueResult,
  TaskState,
} from '../../frontend/src/rpc/gitplus/task/v1/task_pb';
import { nowIso, parseJsonObject, toTimestamp } from './util';
import type { EventBus } from './event-bus';
import type {
  Task,
  TaskProgress,
  TaskRunLog,
} from '../../frontend/src/rpc/gitplus/task/v1/task_pb';
import type {
  AppDatabase,
  JsonRecord,
  TaskRunLogRow,
  TaskRunRow,
} from './types';

export const TASK_CHANNEL = 'task';
export const JOB_TYPE_SYNC_ALL = 'sync-all';
export const JOB_TYPE_SYNC_SOURCE = 'sync-source';
export const JOB_ID_SYNC_ALL = 'sync-all';

export type TaskSnapshot = {
  taskId: string;
  parentTaskId: string;
  jobId: string;
  jobType: string;
  name: string;
  args: JsonRecord | undefined;
  state: 'queued' | 'running' | 'finished' | 'failed';
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  errorMessage?: string;
  progress?: {
    summary: string;
    meta?: JsonRecord;
    updatedAt: string;
  };
};

export type TaskContext = {
  taskId: string;
  setProgress: (summary: string, meta?: JsonRecord) => Promise<void>;
};

export type TaskSpec = {
  parentTaskId?: string;
  jobId: string;
  jobType: string;
  name: string;
  args?: JsonRecord;
  run: (context: TaskContext) => Promise<void>;
};

type TaskEntry = TaskSnapshot & {
  run: (context: TaskContext) => Promise<void>;
};

export class TaskManager {
  #running: TaskEntry | undefined;
  #queue: Array<TaskEntry> = [];
  #closed = false;

  constructor(
    private readonly db: AppDatabase,
    private readonly bus: EventBus,
  ) {}

  enqueue(spec: TaskSpec): { result: TaskEnqueueResult; task: TaskSnapshot } {
    if (this.#closed)
      throw new ConnectError('task manager is closed', Code.Internal);
    validateSpec(spec);

    const queuedDuplicate = this.#queue.find(
      (task) => task.jobId === spec.jobId,
    );
    if (queuedDuplicate) {
      return {
        result: TaskEnqueueResult.DEDUPED,
        task: snapshot(queuedDuplicate),
      };
    }

    const createdAt = nowIso();
    const entry: TaskEntry = {
      taskId: randomUUID(),
      parentTaskId: spec.parentTaskId ?? '',
      jobId: spec.jobId,
      jobType: spec.jobType,
      name: spec.name,
      args: spec.args,
      state: 'queued',
      createdAt,
      run: spec.run,
    };

    this.publish('task.enqueued', entry);

    if (!this.#running) {
      this.start(entry);
      return { result: TaskEnqueueResult.STARTED, task: snapshot(entry) };
    }

    this.#queue.push(entry);
    return { result: TaskEnqueueResult.QUEUED, task: snapshot(entry) };
  }

  runtime(): { runningTask?: TaskSnapshot; queuedTasks: Array<TaskSnapshot> } {
    return {
      runningTask: this.#running ? snapshot(this.#running) : undefined,
      queuedTasks: this.#queue.map(snapshot),
    };
  }

  cancelQueuedTask(taskId: string): TaskSnapshot {
    const index = this.#queue.findIndex((task) => task.taskId === taskId);
    if (index < 0) {
      if (this.#running?.taskId === taskId) {
        throw new ConnectError(
          `task ${taskId} is not queued`,
          Code.FailedPrecondition,
        );
      }
      throw new ConnectError(`task ${taskId} was not found`, Code.NotFound);
    }
    const [task] = this.#queue.splice(index, 1);
    this.publish('task.canceled', task);
    return snapshot(task);
  }

  listTaskRuns(
    pageSize: number,
    offset: number,
    jobType = '',
    parentTaskId = '',
  ) {
    const where: Array<string> = [];
    const params: Record<string, unknown> = { limit: pageSize, offset };
    if (jobType) {
      where.push('job_type = @jobType');
      params.jobType = jobType;
    }
    if (parentTaskId) {
      where.push('parent_task_id = @parentTaskId');
      params.parentTaskId = parentTaskId;
    }
    const whereSql = where.length > 0 ? `WHERE ${where.join(' AND ')}` : '';
    const total = this.db
      .prepare(`SELECT COUNT(1) AS count FROM task_runs ${whereSql}`)
      .get(params) as { count: number };
    const rows = this.db
      .prepare(
        `
        SELECT * FROM task_runs
        ${whereSql}
        ORDER BY started_at DESC, task_id DESC
        LIMIT @limit OFFSET @offset
      `,
      )
      .all(params) as Array<TaskRunRow>;
    return { totalCount: total.count, tasks: rows.map(taskRunRowToProto) };
  }

  getTaskRun(taskId: string): Task {
    const row = this.db
      .prepare('SELECT * FROM task_runs WHERE task_id = ? LIMIT 1')
      .get(taskId) as TaskRunRow | undefined;
    if (!row)
      throw new ConnectError(`task run ${taskId} was not found`, Code.NotFound);
    return taskRunRowToProto(row);
  }

  listTaskRunLogs(taskId: string): Array<TaskRunLog> {
    this.getTaskRun(taskId);
    const rows = this.db
      .prepare(
        'SELECT * FROM task_run_logs WHERE task_id = ? ORDER BY created_at, id',
      )
      .all(taskId) as Array<TaskRunLogRow>;
    return rows.map(taskRunLogRowToProto);
  }

  close(): void {
    this.#closed = true;
  }

  private start(entry: TaskEntry): void {
    entry.state = 'running';
    entry.startedAt = nowIso();
    this.#running = entry;
    this.recordStarted(entry);
    this.publish('task.started', entry);

    void this.runEntry(entry);
  }

  private async runEntry(entry: TaskEntry): Promise<void> {
    const context: TaskContext = {
      taskId: entry.taskId,
      setProgress: async (summary, meta) => {
        entry.progress = {
          summary,
          meta,
          updatedAt: nowIso(),
        };
        this.recordProgress(entry);
        this.publish('task.progress', entry);
        await Promise.resolve();
      },
    };

    try {
      await entry.run(context);
      entry.state = 'finished';
      entry.finishedAt = nowIso();
      this.recordFinished(entry);
      this.publish('task.finished', entry);
    } catch (error) {
      entry.state = 'failed';
      entry.finishedAt = nowIso();
      entry.errorMessage =
        error instanceof Error ? error.message : String(error);
      this.recordFailed(entry);
      this.publish('task.failed', entry);
    } finally {
      if (this.#running?.taskId === entry.taskId) {
        this.#running = undefined;
      }
      const next = this.#queue.shift();
      if (next) this.start(next);
    }
  }

  private recordStarted(entry: TaskEntry): void {
    this.db
      .prepare(
        `
        INSERT INTO task_runs (
          task_id, parent_task_id, job_id, job_type, name, args_json, status,
          created_at, started_at, finished_at, error_message,
          last_progress_summary, last_progress_meta_json, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, ?)
      `,
      )
      .run(
        entry.taskId,
        entry.parentTaskId || null,
        entry.jobId,
        entry.jobType,
        entry.name,
        entry.args ? JSON.stringify(entry.args) : null,
        entry.state,
        entry.createdAt,
        entry.startedAt,
        entry.startedAt,
      );
    this.recordLog(entry, 'task_started');
  }

  private recordProgress(entry: TaskEntry): void {
    this.db
      .prepare(
        `
        UPDATE task_runs
        SET last_progress_summary = ?, last_progress_meta_json = ?, updated_at = ?
        WHERE task_id = ?
      `,
      )
      .run(
        entry.progress?.summary ?? null,
        entry.progress?.meta ? JSON.stringify(entry.progress.meta) : null,
        entry.progress?.updatedAt ?? nowIso(),
        entry.taskId,
      );
    this.recordLog(entry, 'task_progress');
  }

  private recordFinished(entry: TaskEntry): void {
    this.db
      .prepare(
        `
        UPDATE task_runs
        SET status = 'finished', finished_at = ?, error_message = NULL, updated_at = ?
        WHERE task_id = ?
      `,
      )
      .run(entry.finishedAt, entry.finishedAt, entry.taskId);
    this.recordLog(entry, 'task_finished');
  }

  private recordFailed(entry: TaskEntry): void {
    this.db
      .prepare(
        `
        UPDATE task_runs
        SET status = 'failed', finished_at = ?, error_message = ?, updated_at = ?
        WHERE task_id = ?
      `,
      )
      .run(
        entry.finishedAt,
        entry.errorMessage,
        entry.finishedAt,
        entry.taskId,
      );
    this.recordLog(entry, 'task_failed');
  }

  private recordLog(entry: TaskEntry, eventType: string): void {
    this.db
      .prepare(
        `
        INSERT INTO task_run_logs (
          task_id, event_type, summary, meta_json, error_message, created_at
        ) VALUES (?, ?, ?, ?, ?, ?)
      `,
      )
      .run(
        entry.taskId,
        eventType,
        entry.progress?.summary ?? null,
        entry.progress?.meta ? JSON.stringify(entry.progress.meta) : null,
        entry.errorMessage ?? null,
        nowIso(),
      );
  }

  private publish(name: string, task: TaskSnapshot): void {
    this.bus.publish({
      channel: TASK_CHANNEL,
      name,
      event: {
        name,
        task: protoTaskToJson(toProtoTask(task)),
        occurred_at: nowIso(),
        error_message: task.errorMessage ?? '',
      },
    });
  }
}

export function toProtoTask(task: TaskSnapshot): Task {
  return {
    taskId: task.taskId,
    parentTaskId: task.parentTaskId,
    jobId: task.jobId,
    jobType: task.jobType,
    name: task.name,
    state: toProtoState(task.state),
    createdAt: toTimestamp(task.createdAt),
    startedAt: toTimestamp(task.startedAt),
    finishedAt: toTimestamp(task.finishedAt),
    errorMessage: task.errorMessage ?? '',
    args: task.args,
    progress: task.progress
      ? ({
          summary: task.progress.summary,
          meta: task.progress.meta,
          updatedAt: toTimestamp(task.progress.updatedAt),
        } as TaskProgress)
      : undefined,
  } as Task;
}

function validateSpec(spec: TaskSpec): void {
  if (!spec.jobId.trim())
    throw new ConnectError('job_id is required', Code.InvalidArgument);
  if (!spec.jobType.trim())
    throw new ConnectError('job_type is required', Code.InvalidArgument);
  if (!spec.name.trim())
    throw new ConnectError('task name is required', Code.InvalidArgument);
}

function snapshot(task: TaskEntry): TaskSnapshot {
  const { run: _run, ...rest } = task;
  void _run;
  return { ...rest };
}

function toProtoState(state: TaskSnapshot['state']): TaskState {
  switch (state) {
    case 'queued':
      return TaskState.QUEUED;
    case 'running':
      return TaskState.RUNNING;
    case 'finished':
      return TaskState.FINISHED;
    case 'failed':
      return TaskState.FAILED;
  }
}

function taskRunRowToProto(row: TaskRunRow): Task {
  return {
    taskId: row.task_id,
    parentTaskId: row.parent_task_id ?? '',
    jobId: row.job_id,
    jobType: row.job_type,
    name: row.name,
    state: toProtoState(row.status as TaskSnapshot['state']),
    createdAt: toTimestamp(row.created_at),
    startedAt: toTimestamp(row.started_at),
    finishedAt: toTimestamp(row.finished_at),
    errorMessage: row.error_message ?? '',
    args: parseJsonObject(row.args_json),
    progress:
      row.last_progress_summary || row.last_progress_meta_json
        ? ({
            summary: row.last_progress_summary ?? '',
            meta: parseJsonObject(row.last_progress_meta_json),
            updatedAt: toTimestamp(row.updated_at),
          } as TaskProgress)
        : undefined,
  } as Task;
}

function taskRunLogRowToProto(row: TaskRunLogRow): TaskRunLog {
  return {
    id: BigInt(row.id),
    taskId: row.task_id,
    eventType: row.event_type,
    summary: row.summary ?? '',
    meta: parseJsonObject(row.meta_json),
    errorMessage: row.error_message ?? '',
    createdAt: toTimestamp(row.created_at),
  } as TaskRunLog;
}

function protoTaskToJson(task: Task): JsonRecord {
  return {
    task_id: task.taskId,
    job_id: task.jobId,
    job_type: task.jobType,
    name: task.name,
    state: task.state,
    progress: task.progress
      ? {
          summary: task.progress.summary,
          meta: task.progress.meta,
        }
      : undefined,
  };
}
