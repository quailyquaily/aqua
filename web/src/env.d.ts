/// <reference types="vite/client" />

declare module 'quail-ui'

interface ImportMetaEnv {
  readonly VITE_AQUA_API_BASE?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
