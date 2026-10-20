import type { BrowserWindow, IpcMainInvokeEvent } from 'electron'

export function authorizedSender(event: IpcMainInvokeEvent, window: BrowserWindow | null): boolean {
  return window !== null && !window.isDestroyed() &&
    event.sender === window.webContents &&
    event.senderFrame === window.webContents.mainFrame &&
    event.senderFrame.url === window.webContents.getURL()
}

export function externalHTTPS(raw: unknown): string {
  if (typeof raw !== 'string' || raw.length > 2048) throw new Error('Invalid external link')
  let parsed: URL
  try { parsed = new URL(raw) } catch { throw new Error('Invalid external link') }
  if (parsed.protocol !== 'https:' || !parsed.hostname || parsed.username || parsed.password) {
    throw new Error('External link must use HTTPS')
  }
  return parsed.toString()
}

export function safeUnsigned(value: unknown): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
    throw new Error('Unsafe numeric identity')
  }
  return value
}
