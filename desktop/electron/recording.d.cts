export const MAX_NAMES: number
export interface RecordingPreference {
  enabled: boolean
  redactEnv: string[]
}
export type RecordingSave = { ok: true; settings: RecordingPreference } | { ok: false; code: 'invalid' | 'save_failed'; name?: string }
export interface RecordingSettingsStore {
  load(): RecordingPreference
  save(input: unknown): RecordingSave
}
export function createRecordingSettings(file: string): RecordingSettingsStore
export function validate(input: unknown): { settings: RecordingPreference; invalid?: undefined } | { settings?: undefined; invalid: string }
export function serveFlags(settings: RecordingPreference, env: Record<string, string | undefined>): { args: string[]; recording: boolean; missing: string[] }
