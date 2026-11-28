import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// base './' lets Electron load dist/index.html from the file system.
export default defineConfig({
  base: './',
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true, target: 'chrome140' },
  // src/templates.ts imports ../../.agents/templates/*.yaml?raw, outside this package. Vite's dev and
  // test server only serves files under its workspace root (here: desktop/), so the jsdom tests, which
  // load modules through the server, would be denied; allow the repository root.
  server: { fs: { allow: ['..'] } },
  test: { environment: 'node', include: ['test/**/*.test.{ts,tsx}'] },
})
