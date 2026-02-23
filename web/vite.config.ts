import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import path from 'path';

export default defineConfig({
  plugins: [
    tailwindcss(),
    svelte(),
  ],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
    host: "172.17.0.2"
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    emptyOutDir: false, // preserve .gitkeep
  },
  resolve: {
    alias: {
      $lib: path.resolve('./src/lib'),
    },
  },
  preview: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
