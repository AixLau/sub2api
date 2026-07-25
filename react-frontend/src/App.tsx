import { lazy, Suspense, useEffect, type ReactNode } from 'react'
import { HeroSection } from './components/HeroSection'
import {
  FaqSection,
  ServicesSection,
  StatusSection,
} from './components/HomeSections'
import { Navbar } from './components/Navbar'
import { ModelCatalogSection } from './components/ModelCatalogSection'
import { TrustedBySection } from './components/TrustedBySection'
import { applySeoMetadata, getPageMetadata } from './lib/seo'
import { getPersistedDashboardPath } from './lib/authSession'

const AuthPage = lazy(() =>
  import('./components/AuthPage').then((module) => ({ default: module.AuthPage })),
)

const authRoutes = {
  '/login': 'login',
  '/register': 'register',
  '/forgot-password': 'reset-password',
  '/reset-password': 'reset-password',
  '/change-password': 'change-password',
} as const

export const publicPageRoutes = [
  '/model-market',
  '/services',
  '/service-status',
  '/faq',
] as const

function PublicPageLayout({ children }: { children: ReactNode }) {
  return (
    <div className="app-shell">
      <Navbar />
      <main className="public-page-main">{children}</main>
    </div>
  )
}

function NotFoundPage() {
  return (
    <PublicPageLayout>
      <section className="not-found-section" aria-labelledby="not-found-title">
        <p>404</p>
        <h1 id="not-found-title">这个页面不存在。</h1>
        <span>请返回首页，或从菜单查看模型、服务状态与常见问题。</span>
        <a className="inline-action inline-action--dark" href="/">
          返回首页
        </a>
      </section>
    </PublicPageLayout>
  )
}

function PublicBusinessPage({ pathname }: { pathname: string }) {
  switch (pathname) {
    case '/model-market':
      return (
        <PublicPageLayout>
          <ModelCatalogSection />
        </PublicPageLayout>
      )
    case '/services':
      return (
        <PublicPageLayout>
          <ServicesSection />
        </PublicPageLayout>
      )
    case '/service-status':
      return (
        <PublicPageLayout>
          <StatusSection />
        </PublicPageLayout>
      )
    case '/faq':
      return (
        <PublicPageLayout>
          <FaqSection />
        </PublicPageLayout>
      )
    default:
      return <NotFoundPage />
  }
}

function DashboardRedirect({ to }: { to: '/admin/dashboard' | '/dashboard' }) {
  useEffect(() => {
    window.location.replace(to)
  }, [to])

  return null
}

export default function App() {
  const pathname = window.location.pathname
  const authMode = authRoutes[pathname as keyof typeof authRoutes]
  const isHomeRoute = pathname === '/' || pathname === '/home'
  const dashboardPath = isHomeRoute ? getPersistedDashboardPath() : null

  useEffect(() => {
    applySeoMetadata(getPageMetadata(pathname))
  }, [pathname])

  if (authMode) {
    return (
      <Suspense fallback={null}>
        <AuthPage mode={authMode} />
      </Suspense>
    )
  }

  if (dashboardPath) {
    return <DashboardRedirect to={dashboardPath} />
  }

  if (!isHomeRoute) {
    return <PublicBusinessPage pathname={pathname} />
  }

  return (
    <div className="app-shell app-shell--home">
      <Navbar />
      <main className="home-main">
        <HeroSection />
        <TrustedBySection />
      </main>
    </div>
  )
}
