import { createApp } from 'vue'
import PrimeVue from 'primevue/config'
import ToastService from 'primevue/toastservice'
import ConfirmationService from 'primevue/confirmationservice'
import Aura from '@primeuix/themes/aura'
import '@fontsource/lato/400.css'
import '@fontsource/lato/700.css'
import 'primeicons/primeicons.css'
import './assets/tailwind.css'
import './assets/layout/layout.scss'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { initializeFont } from './shared/fonts'

initializeFont()
createApp(App)
  .use(router)
  .use(ToastService)
  .use(ConfirmationService)
  .use(PrimeVue, {
    theme: { preset: Aura, options: { darkModeSelector: '.app-dark' } },
  })
  .mount('#app')
