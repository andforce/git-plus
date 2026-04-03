import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { eventClient } from './connect/client';

export function useTaskEvents() {
  const queryClient = useQueryClient();
  const activeRef = useRef(false);

  useEffect(() => {
    if (activeRef.current) return;
    activeRef.current = true;

    const controller = new AbortController();

    (async () => {
      try {
        for await (const _event of eventClient.subscribe(
          { channel: 'task' },
          { signal: controller.signal },
        )) {
          queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });
        }
      } catch {
        // AbortError on unmount is expected; ignore all stream errors
        // since the query cache retains the last known state.
      }
    })();

    return () => {
      activeRef.current = false;
      controller.abort();
    };
  }, [queryClient]);
}
