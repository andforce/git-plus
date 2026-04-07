type SourceDisplay = {
  id: string;
  name: string;
  username: string;
};

export function sourcePrimaryLabel(source: SourceDisplay): string {
  return source.name;
}

export function sourceSecondaryLabel(source: SourceDisplay): string | null {
  if (source.name === source.id) {
    return null;
  }

  return source.id;
}

export function sourceListLabel(source: SourceDisplay): string {
  const secondaryLabel = sourceSecondaryLabel(source);
  const primaryLabel = sourcePrimaryLabel(source);

  if (secondaryLabel) {
    return `${primaryLabel} (${secondaryLabel}) — @${source.username}`;
  }

  return `${primaryLabel} — @${source.username}`;
}
