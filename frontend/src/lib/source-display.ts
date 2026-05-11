type SourceDisplay = {
  id: string;
  name: string;
  username: string;
};

export function sourcePrimaryLabel(source: SourceDisplay): string {
  return source.username || source.id;
}

export function sourceSecondaryLabel(_source: SourceDisplay): string | null {
  return null;
}

export function sourceListLabel(source: SourceDisplay): string {
  const primaryLabel = sourcePrimaryLabel(source);

  return source.username ? primaryLabel : source.id;
}
