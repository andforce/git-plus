import { QueryClientProvider } from '@tanstack/react-query';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import { App, queryClient } from './App';
import '@mantine/core/styles.css';
import '@mantine/nprogress/styles.css';
import 'sonner/dist/styles.css';
import '~ui/theme/style.css';
import '~styles.css';

dayjs.extend(relativeTime);

const rootElement = document.getElementById('app');

if (!rootElement) {
  throw new Error('Root element #app was not found.');
}

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
      <Toaster position="top-center" richColors />
    </QueryClientProvider>
  </StrictMode>,
);
