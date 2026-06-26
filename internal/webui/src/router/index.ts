import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory('/'),
  routes: [
    {
      path: '/',
      name: 'chat',
      component: () => import('@/pages/ChatPage.vue'),
    },
    // Unknown paths fall back to the chat view (SPA single-page behavior).
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
    },
  ],
})

export default router
