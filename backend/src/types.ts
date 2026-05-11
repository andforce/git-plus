import type Database from 'better-sqlite3';

export type AppDatabase = Database.Database;

export type SourceConfig = {
  id: string;
  name?: string;
  platform: 'github';
  username: string;
  token: string;
  only_include_repos: Array<string>;
  exclude_repos: Array<string>;
  include_defaults: boolean;
  include_starred: boolean;
  include_watching: boolean;
};

export type AppConfig = {
  sources: Array<SourceConfig>;
  concurrency: number;
  max_retry_times: number;
  cron?: string;
};

export type RepoRow = {
  id: number;
  source_id: string;
  platform: string;
  ref_id: string;
  status: string;
  name: string;
  full_name: string;
  owner: string;
  description: string | null;
  html_url: string | null;
  clone_url: string | null;
  ssh_url: string | null;
  default_branch: string | null;
  visibility: string | null;
  is_private: number;
  is_fork: number;
  is_archived: number;
  origin: string;
  meta: string;
  archive_repo_size_bytes: number | null;
  last_seen_at: string;
  disabled_at: string | null;
  created_at: string;
  updated_at: string;
};

export type RepoRefRow = {
  id: number;
  repo_id: number;
  ref_name: string;
  ref_kind: 'head' | 'tag';
  current_hash: string;
  status: 'active' | 'deleted';
  archive_ref_name: string | null;
  first_seen_at: string;
  last_seen_at: string;
  last_hash_updated_at: string;
  current_commit_authored_at: string | null;
  current_commit_committed_at: string | null;
  current_commit_author_name: string | null;
  current_commit_author_email: string | null;
  current_commit_message: string | null;
  deleted_at: string | null;
  created_at: string;
  updated_at: string;
};

export type TaskRunRow = {
  task_id: string;
  parent_task_id: string | null;
  job_id: string;
  job_type: string;
  name: string;
  args_json: string | null;
  status: string;
  created_at: string;
  started_at: string;
  finished_at: string | null;
  error_message: string | null;
  last_progress_summary: string | null;
  last_progress_meta_json: string | null;
  updated_at: string;
};

export type TaskRunLogRow = {
  id: number;
  task_id: string;
  event_type: string;
  summary: string | null;
  meta_json: string | null;
  error_message: string | null;
  created_at: string;
};

export type JsonRecord = Record<string, unknown>;
