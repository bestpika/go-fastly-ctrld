import { defineComponent } from 'vue'
import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import ConfirmDialog from 'primevue/confirmdialog'
import { primePlugins } from '../test/prime'
import { useActionConfirm } from './confirmation'
function setup() {
  const result = vi.fn()
  const Host = defineComponent({
    components: { ConfirmDialog },
    setup() {
      const ask = useActionConfirm()
      return {
        ask: async () => result(await ask('只刪本機結果，不刪規則或復原資料')),
      }
    },
    template:
      '<ConfirmDialog group="app"/><button data-test="ask" @click="ask">測試確認</button>',
  })
  return { w: mount(Host, { global: { plugins: primePlugins } }), result }
}
it('官方 ConfirmDialog 取消不接受，明確確認才接受', async () => {
  const native = vi.spyOn(window, 'confirm')
  const { w, result } = setup()
  await w.find('[data-test=ask]').trigger('click')
  await flushPromises()
  const body = new DOMWrapper(document.body)
  expect(body.find('.p-confirmdialog').text()).toContain('不刪規則')
  await body
    .findAll('button')
    .find((b) => b.text() === '取消')!
    .trigger('click')
  await flushPromises()
  expect(result).toHaveBeenLastCalledWith(false)
  await w.find('[data-test=ask]').trigger('click')
  await flushPromises()
  await body
    .findAll('button')
    .find((b) => b.text() === '確認')!
    .trigger('click')
  await flushPromises()
  expect(result).toHaveBeenLastCalledWith(true)
  expect(native).not.toHaveBeenCalled()
  native.mockRestore()
  w.unmount()
})
it('切頁卸載未決確認時安全取消，不留下等待的套用', async () => {
  const { w, result } = setup()
  await w.find('[data-test=ask]').trigger('click')
  await flushPromises()
  w.unmount()
  await flushPromises()
  expect(result).toHaveBeenCalledExactlyOnceWith(false)
})
