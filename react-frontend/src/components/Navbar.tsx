import { useState } from 'react'
import { ChevronUp } from 'lucide-react'
import { brandName, navLinks } from '../data/alwayzz'

export function Navbar() {
  const [isOpen, setIsOpen] = useState(false)
  const pathname = window.location.pathname

  return (
    <header className="site-header">
      <nav className="navbar" aria-label="主导航">
        <a className="logo" href="/" aria-label={`${brandName} home`}>
          {brandName}
        </a>
        <div className="nav-actions">
          <a className="nav-account-link" href="/login">
            登录
          </a>
          <a className="nav-account-link nav-account-link--strong" href="/register">
            注册
          </a>
          <button
            className="menu-button"
            type="button"
            aria-expanded={isOpen}
            aria-controls="site-drawer"
            onClick={() => setIsOpen((open) => !open)}
          >
            菜单
            <ChevronUp aria-hidden="true" size={16} strokeWidth={2.25} />
          </button>
        </div>
      </nav>

      <div
        className={`drawer-overlay${isOpen ? ' drawer-overlay--open' : ''}`}
        id="site-drawer"
        aria-hidden={!isOpen}
      >
        <button
          className="drawer-close"
          type="button"
          aria-label="关闭菜单"
          onClick={() => setIsOpen(false)}
        >
          关闭
        </button>
        <div className="drawer-inner">
          <ul className="drawer-links">
            {navLinks.map((link) => (
              <li key={link.href}>
                <a
                  href={link.href}
                  aria-current={pathname === link.href ? 'page' : undefined}
                  onClick={() => setIsOpen(false)}
                >
                  {link.label}
                </a>
              </li>
            ))}
          </ul>
          <p className="drawer-footer">© 2026 星链 · 模型 API 服务</p>
        </div>
      </div>
    </header>
  )
}
