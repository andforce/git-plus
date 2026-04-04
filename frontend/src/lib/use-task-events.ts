import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { eventClient } from './connect/client';

const STATE_CHANGE_EVENTS = new Set([
  'task.enqueued',
  'task.started',
  'task.canceled',
  'task.finished',
  'task.failed',
]);

export function useTaskEvents(watchTaskId?: string) {
  const queryClient = useQueryClient();
  const watchRef = useRef(watchTaskId);
  watchRef.current = watchTaskId;

  useEffect(() => {
    const controller = new AbortController();

    (async () => {
      try {
        for await (const event of eventClient.subscribe(
          { channel: 'task' },
          { signal: controller.signal },
        )) {
          queryClient.invalidateQueries({ queryKey: ['task', 'runtime'] });

          const eventName = event.event?.['event_name'];
          if (
            typeof eventName === 'string' &&
            STATE_CHANGE_EVENTS.has(eventName)
          ) {
            queryClient.invalidateQueries({ queryKey: ['task', 'runs'] });
          }

          const currentWatch = watchRef.current;
          if (currentWatch && event.event?.['task_id'] === currentWatch) {
            queryClient.invalidateQueries({
              queryKey: ['task', 'run', currentWatch],
            });
          }
        }
      } catch {
        // AbortError on unmount is expected; ignore all stream errors
        // since the query cache retains the last known state.
      }
    })();

    return () => {
      controller.abort();
    };
  }, [queryClient]);
}
