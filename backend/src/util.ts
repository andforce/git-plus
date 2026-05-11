import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import type { Timestamp } from '@bufbuild/protobuf/wkt';
import type { JsonRecord } from './types';

export function nowIso(): string {
  return new Date().toISOString();
}

export function toTimestamp(
  value: string | null | undefined,
): Timestamp | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return undefined;
  return timestampFromDate(date);
}

export function parseJsonObject(
  value: string | null | undefined,
): JsonRecord | undefined {
  if (!value) return undefined;
  try {
    const parsed = JSON.parse(value) as unknown;
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as JsonRecord;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

export function encodePageToken(offset: number): string {
  return Buffer.from(String(offset), 'utf8').toString('base64url');
}

export function decodePageToken(token: string): number {
  const trimmed = token.trim();
  if (!trimmed) return 0;
  const decoded = Buffer.from(trimmed, 'base64url').toString('utf8');
  const offset = Number.parseInt(decoded, 10);
  if (!Number.isFinite(offset) || offset < 0) {
    throw new Error('invalid page token');
  }
  return offset;
}

export function compactString(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

export function nonEmptyArray(
  values: Array<string> | undefined,
): Array<string> {
  if (!values) return [];
  return values.map((value) => value.trim()).filter(Boolean);
}

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function asError(value: unknown): Error {
  return value instanceof Error ? value : new Error(String(value));
}
