import { describe, expect, it } from 'vitest'
import { VIDEO_BASE_URLS, resolveVideoProtocol, switchVideoBaseURL, videoBaseURL } from '../videoProtocol'

describe('videoProtocol', () => {
  it('videoBaseURL 返回各协议默认地址，未知协议回落 seedance', () => {
    expect(videoBaseURL('seedance')).toBe('https://ark.cn-beijing.volces.com/api/v3')
    expect(videoBaseURL('gemini')).toBe('https://generativelanguage.googleapis.com')
    expect(videoBaseURL('minimax')).toBe('https://api.minimax.io')
    expect(videoBaseURL('unknown')).toBe(VIDEO_BASE_URLS.seedance)
  })

  it('resolveVideoProtocol 只接受已知协议，其余回落 seedance', () => {
    expect(resolveVideoProtocol('minimax')).toBe('minimax')
    expect(resolveVideoProtocol('gpt')).toBe('seedance')
    expect(resolveVideoProtocol(undefined)).toBe('seedance')
    expect(resolveVideoProtocol(null)).toBe('seedance')
  })

  it('switchVideoBaseURL 在空值时替换为新协议默认地址', () => {
    expect(switchVideoBaseURL('', 'gemini')).toBe(videoBaseURL('gemini'))
    expect(switchVideoBaseURL('   ', 'minimax')).toBe(videoBaseURL('minimax'))
  })

  it('switchVideoBaseURL 将其它协议的官方默认地址替换为新协议地址', () => {
    expect(switchVideoBaseURL('https://ark.cn-beijing.volces.com/api/v3', 'gemini')).toBe(videoBaseURL('gemini'))
    expect(switchVideoBaseURL('https://api.minimax.io', 'seedance')).toBe(videoBaseURL('seedance'))
  })

  it('switchVideoBaseURL 归一化尾斜杠后再比较', () => {
    expect(switchVideoBaseURL('https://ark.cn-beijing.volces.com/api/v3/', 'minimax')).toBe(videoBaseURL('minimax'))
    expect(switchVideoBaseURL(' https://generativelanguage.googleapis.com/ ', 'seedance')).toBe(videoBaseURL('seedance'))
  })

  it('switchVideoBaseURL 将 api.minimaxi.com 视为 minimax 默认地址可替换', () => {
    expect(switchVideoBaseURL('https://api.minimaxi.com', 'gemini')).toBe(videoBaseURL('gemini'))
    expect(switchVideoBaseURL('https://api.minimaxi.com/', 'seedance')).toBe(videoBaseURL('seedance'))
    expect(switchVideoBaseURL('https://api.minimaxi.com', 'minimax')).toBe(videoBaseURL('minimax'))
  })

  it('switchVideoBaseURL 保留自定义中转地址', () => {
    expect(switchVideoBaseURL('https://video-relay.example', 'gemini')).toBe('https://video-relay.example')
    expect(switchVideoBaseURL('https://metaso.cn/api/minimax/v2', 'seedance')).toBe('https://metaso.cn/api/minimax/v2')
    expect(switchVideoBaseURL('https://api.minimaxi.com/v2', 'seedance')).toBe('https://api.minimaxi.com/v2')
  })
})
