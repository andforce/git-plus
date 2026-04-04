import { sql } from 'drizzle-orm';
import {
  check,
  index,
  integer,
  sqliteTable,
  text,
  uniqueIndex,
} from 'drizzle-orm/sqlite-core';

export const appMeta = sqliteTable('app_meta', {
  key: text('key').primaryKey(),
  value: text('value').notNull(),
});

export const repos = sqliteTable(
  'repos',
  {
    id: integer('id').primaryKey({ autoIncrement: true }),
    sourceId: text('source_id').notNull(),
    platform: text('platform').notNull(),
    refId: text('ref_id').notNull(),
    status: text('status').notNull().default('active'),
    name: text('name').notNull(),
    fullName: text('full_name').notNull(),
    owner: text('owner').notNull(),
    description: text('description'),
    htmlUrl: text('html_url'),
    cloneUrl: text('clone_url'),
    sshUrl: text('ssh_url'),
    defaultBranch: text('default_branch'),
    visibility: text('visibility'),
    isPrivate: integer('is_private').notNull().default(0),
    isFork: integer('is_fork').notNull().default(0),
    isArchived: integer('is_archived').notNull().default(0),
    origin: text('origin').notNull().default('{}'),
    meta: text('meta').notNull(),
    lastSeenAt: text('last_seen_at').notNull(),
    disabledAt: text('disabled_at'),
    createdAt: text('created_at')
      .notNull()
      .default(sql`CURRENT_TIMESTAMP`),
    updatedAt: text('updated_at')
      .notNull()
      .default(sql`CURRENT_TIMESTAMP`),
  },
  (table) => [
    uniqueIndex('repos_source_ref_unique').on(table.sourceId, table.refId),
    index('repos_source_status_idx').on(table.sourceId, table.status),
    index('repos_source_full_name_idx').on(table.sourceId, table.fullName),
    check(
      'repos_status_check',
      sql`${table.status} IN ('active', 'auto_excluded')`,
    ),
    check('repos_origin_json_check', sql`json_valid(${table.origin})`),
    check('repos_meta_json_check', sql`json_valid(${table.meta})`),
  ],
);

export const taskRuns = sqliteTable(
  'task_runs',
  {
    taskId: text('task_id').primaryKey(),
    parentTaskId: text('parent_task_id'),
    jobId: text('job_id').notNull(),
    jobType: text('job_type').notNull(),
    name: text('name').notNull(),
    argsJson: text('args_json'),
    status: text('status').notNull(),
    createdAt: text('created_at').notNull(),
    startedAt: text('started_at').notNull(),
    finishedAt: text('finished_at'),
    errorMessage: text('error_message'),
    lastProgressSummary: text('last_progress_summary'),
    lastProgressMetaJson: text('last_progress_meta_json'),
    updatedAt: text('updated_at').notNull(),
  },
  (table) => [
    index('task_runs_started_at_idx').on(table.startedAt),
    index('task_runs_job_type_idx').on(table.jobType),
    index('task_runs_parent_task_id_idx').on(table.parentTaskId),
  ],
);

export const taskRunLogs = sqliteTable(
  'task_run_logs',
  {
    id: integer('id').primaryKey({ autoIncrement: true }),
    taskId: text('task_id')
      .notNull()
      .references(() => taskRuns.taskId, { onDelete: 'cascade' }),
    eventType: text('event_type').notNull(),
    summary: text('summary'),
    metaJson: text('meta_json'),
    errorMessage: text('error_message'),
    createdAt: text('created_at').notNull(),
  },
  (table) => [
    index('task_run_logs_task_id_created_at_idx').on(
      table.taskId,
      table.createdAt,
      table.id,
    ),
  ],
);
