import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// base './' lets Electron load dist/index.html from the file system.
export default defineConfig({
  base: './',
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true, target: 'chrome140' },
  test: { environment: 'node', include: ['test/**/*.test.{ts,tsx}'] },
})
