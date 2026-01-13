import { createRouter, createWebHistory } from 'vue-router'
import ReleaseListView from '../views/ReleaseListView.vue'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      redirect: '/releases'
    },
    {
      path: '/releases',
      name: 'releases',
      component: ReleaseListView
    },
    {
      path: '/releases/:id',
      name: 'release-detail',
      component: () => import('../views/ReleaseDetailView.vue')
    }
  ]
})

export default router
