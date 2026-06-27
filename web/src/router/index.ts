import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'Home',
      component: () => import('@/views/HomeView.vue'),
    },
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/LoginView.vue'),
    },
    {
      path: '/register',
      name: 'Register',
      component: () => import('@/views/RegisterView.vue'),
    },
    {
      path: '/threads',
      name: 'ThreadList',
      component: () => import('@/views/ThreadListView.vue'),
    },
    {
      path: '/threads/:id',
      name: 'ThreadDetail',
      component: () => import('@/views/ThreadDetailView.vue'),
    },
    {
      path: '/threads/create',
      name: 'CreateThread',
      component: () => import('@/views/CreateThreadView.vue'),
      meta: { requiresAuth: true },
    },
  ],
})

// 路由守卫
router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('access_token')

  if (to.meta.requiresAuth && !token) {
    next({ path: '/login', query: { redirect: to.fullPath } })
    return
  }

  next()
})

export default router
