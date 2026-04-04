import { DownloadStage, DownloadState } from '~rpc/gitplus/repo/v1/repo_pb';

export function formatEstimatedBytes(value: bigint | undefined) {
  if (!value || value <= 0n) return 'Estimate unavailable';

  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let size = Number(value);
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  const precision = size >= 100 ? 0 : size >= 10 ? 1 : 2;
  return `${size.toFixed(precision)} ${units[unitIndex]}`;
}

export function estimateProcessingTime(sizeBytes: bigint | undefined) {
  if (!sizeBytes || sizeBytes <= 0n) return 'Estimate unavailable';

  const miB = 1024n * 1024n;
  const giB = 1024n * miB;

  if (sizeBytes <= 50n * miB) return '< 10s';
  if (sizeBytes <= 250n * miB) return '10-30s';
  if (sizeBytes <= giB) return '30-90s';
  return '1-3 min';
}

export function estimatedDownloadSize(sizeBytes: bigint | undefined) {
  if (!sizeBytes || sizeBytes <= 0n) return undefined;
  return (sizeBytes * 7n) / 10n;
}

export function downloadStageLabel(stage: DownloadStage) {
  switch (stage) {
    case DownloadStage.COPY_BARE:
      return 'Copying bare repository';
    case DownloadStage.MATERIALIZE_REFS:
      return 'Restoring active refs';
    case DownloadStage.PACKAGE_ZIP:
      return 'Packaging zip archive';
    case DownloadStage.READY:
      return 'Ready to download';
    default:
      return 'Preparing download';
  }
}

export function downloadStateLabel(state: DownloadState) {
  switch (state) {
    case DownloadState.RUNNING:
      return 'Preparing';
    case DownloadState.READY:
      return 'Ready';
    case DownloadState.FAILED:
      return 'Failed';
    default:
      return 'Idle';
  }
}

export function downloadStateColor(state: DownloadState) {
  switch (state) {
    case DownloadState.RUNNING:
      return 'blue';
    case DownloadState.READY:
      return 'teal';
    case DownloadState.FAILED:
      return 'red';
    default:
      return 'gray';
  }
}
