import type { APIErrorEnvelope } from './types'

const rawAPIBase = typeof import.meta !== 'undefined' ? import.meta.env.VITE_AQUA_API_BASE?.trim() : ''
const defaultAPIPort = '8089'

function resolveHTTPBase(): URL {
  const protocol = typeof window !== 'undefined' ? window.location.protocol : 'http:'
  const hostname = typeof window !== 'undefined' ? window.location.hostname : '127.0.0.1'
  const fallback = `${protocol}//${hostname}:${defaultAPIPort}`
  return rawAPIBase ? new URL(rawAPIBase, fallback) : new URL(fallback)
}

export class APIError extends Error {
  code: string
  details?: string
  suggestion?: string

  constructor(payload: APIErrorEnvelope['error']) {
    super(payload.message)
    this.code = payload.code
    this.details = payload.details
    this.suggestion = payload.suggestion
  }
}

export async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {})
    },
    ...init
  })

  if (!response.ok) {
    let payload: APIErrorEnvelope | undefined
    try {
      payload = (await response.json()) as APIErrorEnvelope
    } catch {
      payload = undefined
    }
    if (payload?.error) {
      throw new APIError(payload.error)
    }
    throw new Error(`${response.status} ${response.statusText}`)
  }

  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export function apiURL(path: string): string {
  return new URL(path, resolveHTTPBase()).toString()
}

export function apiWebSocketURL(path: string): string {
  const url = new URL(path, resolveHTTPBase())
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return url.toString()
}
