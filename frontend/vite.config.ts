import { defineConfig } from 'vite';
import viteReact from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET;

const config = defineConfig({
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [tailwindcss(), viteReact()],
  server: apiProxyTarget
    ? {
        proxy: {
          '/api': {
            target: apiProxyTarget,
            changeOrigin: true,
          },
        },
      }
    : undefined,
});

export default config;
