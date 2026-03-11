import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { QuailUI, applyTheme } from 'quail-ui'
import 'quail-ui/dist/index.css'
import './style.css'
import App from './App.vue'

applyTheme('morph')

const app = createApp(App)
app.use(createPinia())
app.use(QuailUI)
app.mount('#app')
