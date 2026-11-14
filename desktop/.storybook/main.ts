import type { StorybookConfig } from '@storybook/react-vite'

// Development-only. Storybook is not part of the packaged app or the renderer
// bundle (scripts/check-bundle.mjs enforces that), makes no network requests and
// sends no telemetry.
const config: StorybookConfig = {
  stories: ['../src/**/*.stories.tsx'],
  addons: ['@storybook/addon-a11y'],
  framework: '@storybook/react-vite',
  core: { disableTelemetry: true, disableWhatsNewNotifications: true },
  viteFinal: async (c) => {
    // Stories read the demo graph from the repository's .agents/graphs.
    c.server = { ...c.server, fs: { ...c.server?.fs, allow: ['..'] } }
    return c
  },
}
export default config
