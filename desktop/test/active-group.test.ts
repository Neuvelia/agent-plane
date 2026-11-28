import { describe, expect, it } from 'vitest'
import { activeGroup, isAtEnd } from '../src/settings/activeGroup'

const groups = [
  { id: 'recording', top: 300 },
  { id: 'execution', top: 40 },
  { id: 'engine', top: 650 },
]

describe('activeGroup', () => {
  it('[F2.5f] the visible group nearest the top wins', () => {
    expect(activeGroup(groups, false)).toBe('execution')
  })

  it('[F2.5f] at the end of the page the last visible group wins, not the nearest the top', () => {
    expect(activeGroup([groups[1]!, groups[2]!], true)).toBe('engine')
  })

  it('[F2.5f] nothing visible means no change', () => {
    expect(activeGroup([], true)).toBeNull()
    expect(activeGroup([], false)).toBeNull()
  })
})

describe('isAtEnd', () => {
  it('[F2.5f] is true within one pixel of the end and false above it', () => {
    expect(isAtEnd({ scrollTop: 399, clientHeight: 500, scrollHeight: 900 })).toBe(true)
    expect(isAtEnd({ scrollTop: 398, clientHeight: 500, scrollHeight: 900 })).toBe(false)
  })
})
