const { join } = require('node:path')

const platform = `${process.platform}-${process.arch}`
const executable = process.platform === 'win32' ? 'agentplane-desktop.exe' : 'agentplane-desktop'

module.exports = {
  appId: 'dev.agentplane.desktop',
  productName: 'AgentPlane',
  asar: true,
  electronDist: join(__dirname, 'node_modules', 'electron', 'dist'),
  directories: { output: 'dist' },
  files: [
    'out/**/*',
    'package.json',
    'THIRD_PARTY_NOTICES.md',
    'licenses/**/*',
    '!node_modules/**/*'
  ],
  extraResources: [{
    from: join(__dirname, 'build', 'sidecar', platform, executable),
    to: join('agentplane', 'bin', platform, executable)
  }],
  mac: { target: ['dir'], identity: null },
  win: { target: ['dir'], signExecutable: false },
  linux: { target: ['dir'], category: 'Development' }
}
