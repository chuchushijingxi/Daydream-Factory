import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { copyFileSync, mkdirSync, rmSync } from 'node:fs';

// 只发布实际运行需要的资源，避免本地参考材料随 public 目录整包进入应用。
const publicFiles = [
  'fonts/MiSans-LICENSE.pdf',
  'fonts/NOTICE.txt',
  'fonts/misans-manifest.json',
  'images/placeholder.png',
  'images/brand-logo.png',
  'images/image-types/closeup-main-sample.webp',
  'images/image-types/closeup-sample.webp',
  'images/image-types/closeup-three-view-sample.webp',
  'images/image-types/main-view-sample.webp',
  'images/image-types/three-view-sample.webp',
  'images/xiaoyunque-upload/script-page-1.png',
  'images/xiaoyunque-upload/script-page-2.png',
  'images/xiaoyunque-upload/tab-curve.svg',
  'images/xiaoyunque-upload/upload.svg',
];

// 工作台仅保留 /demo/app/ 入口。
export default defineConfig({
  base: '/demo/',
  publicDir: false,
  plugins: [
    react(),
    tailwindcss(),
    {
      name: 'workspace-static-entries',
      enforce: 'post',
      buildStart() {
        // app 是可重建的产物；先移除旧产物，防止已退出的法务页继续被部署。
        rmSync(new URL('../demo/app/', import.meta.url), { recursive: true, force: true });
      },
      generateBundle(_, bundle) {
        const entry = bundle['index.html'];
        if (entry) {
          delete bundle['index.html'];
          entry.fileName = 'app/index.html';
          bundle[entry.fileName] = entry;
        }
      },
      writeBundle() {
        for (const file of publicFiles) {
          const target = new URL('../demo/app/' + file, import.meta.url);
          mkdirSync(new URL('.', target), { recursive: true });
          copyFileSync(new URL('./public/' + file, import.meta.url), target);
        }
      },
    },
  ],
  envDir: false,
  envPrefix: 'DRAMA_UI_UNUSED_',
  build: {
    outDir: '../demo',
    emptyOutDir: false,
    assetsDir: 'app/assets',
    target: 'es2022',
    rollupOptions: {
      preserveEntrySignatures: 'strict',
      input: {
        app: new URL('./index.html', import.meta.url).pathname,
      },
      output: {
        manualChunks: (id) => (id.includes('/node_modules/') ? 'vendor' : undefined),
        entryFileNames: 'app/assets/[name]-[hash].js',
        chunkFileNames: 'app/assets/[name]-[hash].js',
      },
    },
  },
});
