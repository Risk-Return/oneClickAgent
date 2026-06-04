/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

const API_PREFIX = process.env.VITE_API_PREFIX || '';
const API_TARGET = process.env.VITE_API_TARGET || 'http://localhost:42080';

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  base: process.env.VITE_BASE || '/',
  define: {
    'import.meta.env.VITE_API_PREFIX': JSON.stringify(API_PREFIX),
    'import.meta.env.VITE_BASE': JSON.stringify(process.env.VITE_BASE || '/'),
  },
  build: {
    target: 'es2022',
  },
  server: {
    port: parseInt(process.env.VITE_PORT || '45173'),
    proxy: {
      '/api':         { target: API_TARGET, changeOrigin: true },
      '/ws':          { target: API_TARGET.replace('http', 'ws'), ws: true },
      '/tunnel':      { target: API_TARGET.replace('http', 'ws'), ws: true },
      '/session':     { target: API_TARGET.replace('http', 'ws'), ws: true },
      '/healthz':     { target: API_TARGET, changeOrigin: true },
      '/readyz':      { target: API_TARGET, changeOrigin: true },
      '/metrics':     { target: API_TARGET, changeOrigin: true },
    },
  },
});
