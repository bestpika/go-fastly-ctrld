import { onBeforeUnmount, ref } from 'vue'
import { read, unwrap } from './app'
type Wire<T> = {
  ok: boolean
  data: T
  error: { code: string; message: string; errorId?: string } | null
}
type Request<T> = Promise<Wire<T>> & { cancel?: () => void | Promise<void> }
export type ReadTicket = {
  wait: <U>(p: Request<U>) => ReturnType<typeof unwrap<U>>
  phase: (label: string) => void
  current: () => boolean
}
export function useReadScope() {
  const loading = ref(false),
    phase = ref(''),
    operation = ref('')
  let generation = 0
  const pending = new Set<{ cancel?: () => void | Promise<void> }>()
  function cancel() {
    generation++
    for (const p of pending)
      try {
        void Promise.resolve(p.cancel?.()).catch(() => {})
      } catch {
        /* 已完成的呼叫不需再次取消。 */
      }
    pending.clear()
    loading.value = false
    phase.value = ''
    operation.value = ''
  }
  async function execute<T>(
    label: string,
    work: (ticket: ReadTicket) => Promise<T>,
  ) {
    cancel()
    const id = generation
    const current = () => id === generation
    loading.value = true
    phase.value = label
    operation.value = label
    const stale = () =>
      Object.assign(new Error('讀取已取消'), { name: 'CancelError' })
    const ticket = {
      current,
      phase: (label: string) => {
        if (current()) phase.value = label
      },
      wait: async <U>(p: Request<U>) => {
        if (!current()) {
          void Promise.resolve(p.cancel?.()).catch(() => {})
          throw stale()
        }
        pending.add(p)
        try {
          const value = await unwrap(p)
          if (!current()) throw stale()
          return value
        } finally {
          pending.delete(p)
        }
      },
    }
    try {
      return await read(
        async () => ({ ok: true, data: await work(ticket), error: null }),
        current,
      )
    } finally {
      if (current()) {
        loading.value = false
        phase.value = ''
        operation.value = ''
      }
    }
  }
  onBeforeUnmount(cancel)
  return { loading, phase, operation, cancel, execute }
}
