import { onBeforeUnmount } from 'vue'
import { useConfirm } from 'primevue/useconfirm'

// 只橋接業務確認結果；視窗、焦點、遮罩、Escape 均由官方 ConfirmDialog 處理。
let cancelPending: (() => void) | undefined
export function useActionConfirm() {
  const confirm = useConfirm()
  let ownCancel: (() => void) | undefined
  onBeforeUnmount(() => {
    if (ownCancel) {
      ownCancel()
      confirm.close()
    }
  })
  return (message: string, header = '請確認') =>
    new Promise<boolean>((resolve) => {
      cancelPending?.()
      let done = false
      const finish = (accepted: boolean) => {
        if (done) return
        done = true
        if (cancelPending === ownCancel) cancelPending = undefined
        ownCancel = undefined
        resolve(accepted)
      }
      ownCancel = () => finish(false)
      cancelPending = ownCancel
      confirm.require({
        group: 'app',
        header,
        message,
        icon: 'pi pi-exclamation-triangle',
        defaultFocus: 'reject',
        blockScroll: true,
        rejectProps: { label: '取消', severity: 'secondary', outlined: true },
        acceptProps: { label: '確認' },
        accept: () => finish(true),
        reject: () => finish(false),
        onHide: () => finish(false),
      })
    })
}
