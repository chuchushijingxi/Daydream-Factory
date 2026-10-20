import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
export default defineConfig({
  root: new URL('../', import.meta.url).pathname,
  base: '/demo/__media-live-fixture/',
  publicDir: false,
  plugins: [react(), tailwindcss()],
  build: {
    outDir: new URL('../../demo/__media-live-fixture/', import.meta.url).pathname,
    emptyOutDir: true,
    rollupOptions: { input: new URL('./media-live.html', import.meta.url).pathname },
  },
});
