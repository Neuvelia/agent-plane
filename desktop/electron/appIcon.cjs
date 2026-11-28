'use strict'
const path = require('node:path')

// Tarmind app icon (F2.5a). Paths are fixed and built from __dirname; nothing
// here comes from user input. Packaging is out of scope (D1 = A), so these are
// the runtime icons only; the same files are what a packager would pick up.
const buildDir = path.join(__dirname, '..', 'build')

const ICON_FILES = {
  png: path.join(buildDir, 'icon.png'), // macOS Dock (1024x1024)
  icns: path.join(buildDir, 'icon.icns'), // macOS bundle (packaging only, not used at runtime)
  ico: path.join(buildDir, 'icon.ico'), // Windows window and taskbar
  linux: path.join(buildDir, 'icons', '512x512.png'), // Linux window
}

// BrowserWindow `icon` option. macOS ignores it (the Dock icon is set separately).
function windowIcon(platform) {
  if (platform === 'win32') return ICON_FILES.ico
  if (platform === 'linux') return ICON_FILES.linux
  return undefined
}

module.exports = { ICON_FILES, windowIcon }
