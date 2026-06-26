import { inject, provide, type InjectionKey, type Ref } from 'vue'
import type { ConnectionStatus } from '@/composables/useWebSocket'

const ConnectionKey: InjectionKey<Ref<ConnectionStatus>> = Symbol('connection')

/** Provide the live WebSocket connection status to descendant components. */
export function provideConnection(status: Ref<ConnectionStatus>) {
  provide(ConnectionKey, status)
}

/** Inject the connection status; throws if no provider is mounted above. */
export function useConnection(): Ref<ConnectionStatus> {
  const status = inject(ConnectionKey)
  if (!status) throw new Error('useConnection requires provideConnection in an ancestor')
  return status
}
