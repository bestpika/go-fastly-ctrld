import { createRouter, createWebHashHistory } from 'vue-router'
export const pages = [
  {
    path: '/',
    title: '總覽',
    icon: 'pi pi-home',
    component: () => import('./views/DashboardView.vue'),
  },
  {
    path: '/domains',
    title: '網域管理',
    icon: 'pi pi-globe',
    component: () => import('./views/DomainsView.vue'),
  },
  {
    path: '/tests',
    title: '測速結果',
    icon: 'pi pi-gauge',
    component: () => import('./views/TestsView.vue'),
  },
  {
    path: '/sync',
    title: '規則同步',
    icon: 'pi pi-sync',
    component: () => import('./views/SyncView.vue'),
  },
  {
    path: '/history',
    title: '執行紀錄',
    icon: 'pi pi-history',
    component: () => import('./views/HistoryView.vue'),
  },
  {
    path: '/errors',
    title: '錯誤紀錄',
    icon: 'pi pi-exclamation-circle',
    component: () => import('./views/ErrorsView.vue'),
  },
  {
    path: '/settings',
    title: '設定',
    icon: 'pi pi-cog',
    component: () => import('./views/SettingsView.vue'),
  },
  {
    path: '/about',
    title: '關於與開源授權',
    icon: 'pi pi-info-circle',
    component: () => import('./views/AboutView.vue'),
  },
]
export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    ...pages.map((page) => ({
      path: page.path,
      component: page.component,
      meta: { title: page.title },
    })),
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
