import { describe, expect, it } from 'vitest';
import {
  downloadStageLabel,
  downloadStateColor,
  downloadStateLabel,
  estimateProcessingTime,
  estimatedDownloadSize,
  formatEstimatedBytes,
} from './repo-download';
import { DownloadStage, DownloadState } from '~rpc/gitplus/repo/v1/repo_pb';

describe('repo download helpers', () => {
  it('formats byte estimates and falls back when unavailable', () => {
    expect(formatEstimatedBytes(undefined)).toBe('Estimate unavailable');
    expect(formatEstimatedBytes(10n * 1024n * 1024n)).toBe('10.0 MiB');
  });

  it('derives the download estimate from archive size', () => {
    expect(estimatedDownloadSize(100n)).toBe(70n);
    expect(estimatedDownloadSize(undefined)).toBeUndefined();
  });

  it('maps time estimate buckets', () => {
    expect(estimateProcessingTime(10n * 1024n * 1024n)).toBe('< 10s');
    expect(estimateProcessingTime(200n * 1024n * 1024n)).toBe('10-30s');
    expect(estimateProcessingTime(700n * 1024n * 1024n)).toBe('30-90s');
    expect(estimateProcessingTime(2n * 1024n * 1024n * 1024n)).toBe('1-3 min');
  });

  it('maps stage and state labels', () => {
    expect(downloadStageLabel(DownloadStage.PACKAGE_ZIP)).toBe(
      'Packaging zip archive',
    );
    expect(downloadStateLabel(DownloadState.READY)).toBe('Ready');
    expect(downloadStateColor(DownloadState.FAILED)).toBe('red');
  });
});
