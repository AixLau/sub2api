import { lazy, Suspense, useEffect } from 'react'
import { HeroSection } from './components/HeroSection'
import { Navbar } from './components/Navbar'
import { TrustedBySection } from './components/TrustedBySection'
import { applySeoMetadata, getPageMetadata } from './lib/seo'

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

export default function App() {
  const pathname = window.location.pathname
  const authMode = authRoutes[pathname as keyof typeof authRoutes]

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

  return (
    <div className="app-shell">
      <Navbar />
      <main className="home-main">
        <HeroSection />
        <TrustedBySection />
      </main>
    </div>
  )
}
