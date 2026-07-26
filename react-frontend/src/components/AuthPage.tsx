import type { CSSProperties } from 'react'
import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { ArrowLeft, Eye, EyeOff, X } from 'lucide-react'
import { HERO_BACKGROUND_IMAGE, brandName } from '../data/alwayzz'
import {
  loginWithSub2Api,
  registerWithSub2Api,
  requestPasswordReset,
  resetSub2ApiPassword,
  sendRegisterVerifyCode,
} from '../lib/sub2apiAuth'
import {
  fetchLoginAgreementSettings,
  hasAcceptedLoginAgreement,
  persistLoginAgreementAcceptance,
  type LoginAgreementSettings,
} from '../lib/loginAgreement'
import { HeroCurveLines } from './HeroCurveLines'

type AuthMode = 'login' | 'register' | 'reset-password' | 'change-password'

type AuthPageProps = {
  mode: AuthMode
}

type AuthSideCopy = {
  eyebrow: string
  title: string
  subtitle: string
  features: string[]
}

type InputGroupProps = {
  label: string
  placeholder: string
  id?: string
  type?: string
  inputMode?: React.HTMLAttributes<HTMLInputElement>['inputMode']
  autoComplete?: string
  helper?: string
  value?: string
  readOnly?: boolean
  required?: boolean
  action?: React.ReactNode
  onChange?: (value: string) => void
}

const authCopy = {
  login: {
    eyebrow: 'Welcome back',
    title: '登录星链',
    subtitle: '继续管理你的模型 API 服务、用量与接入配置。',
    button: '登录',
    footerText: '还没有账号？',
    footerLink: '创建账号',
    footerHref: '/register',
  },
  register: {
    eyebrow: 'Create access',
    title: '创建账号',
    subtitle: '填写基础信息，开始接入稳定的模型 API 服务层。',
    button: '创建账号',
    footerText: '已有账号？',
    footerLink: '登录',
    footerHref: '/login',
  },
  'reset-password': {
    eyebrow: 'Recover access',
    title: '找回密码',
    subtitle: '输入账号邮箱，我们会发送密码重置说明。',
    button: '发送重置说明',
    footerText: '想起密码了？',
    footerLink: '返回登录',
    footerHref: '/login',
  },
  'change-password': {
    eyebrow: 'Security',
    title: '修改密码',
    subtitle: '更新你的访问密码，保持账号入口清晰可控。',
    button: '更新密码',
    footerText: '暂不修改？',
    footerLink: '返回首页',
    footerHref: '/',
  },
} satisfies Record<AuthMode, Record<string, string>>

const authSideCopy = {
  login: {
    eyebrow: '模型 API 工作台',
    title: '回到星链服务中枢',
    subtitle: '继续管理 API Key、模型接入与调用用量，让团队在一个入口完成配置与查看。',
    features: ['API Key 管理', '模型统一接入', '调用用量视图'],
  },
  register: {
    eyebrow: '统一身份入口',
    title: '接入从账号开始',
    subtitle: '创建账号后即可进入服务配置、调用管理与用量查看流程。',
    features: ['注册身份', '配置服务', '创建 API Key'],
  },
  'reset-password': {
    eyebrow: '账号恢复入口',
    title: '找回访问权限',
    subtitle: '通过邮箱确认身份，重新回到模型服务管理流程。',
    features: ['邮箱验证', '重置密码', '返回工作台'],
  },
  'change-password': {
    eyebrow: '账号安全入口',
    title: '更新访问密码',
    subtitle: '保持账号入口清晰可靠，让服务配置与调用管理持续可控。',
    features: ['当前密码', '新密码', '确认更新'],
  },
} satisfies Record<AuthMode, AuthSideCopy>

export function AuthPage({ mode }: AuthPageProps) {
  const searchParams = new URLSearchParams(window.location.search)
  const resetEmail = searchParams.get('email') || ''
  const resetToken = searchParams.get('token') || ''
  const affiliateCode = searchParams.get('aff') || searchParams.get('aff_code') || ''
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [verifyCode, setVerifyCode] = useState('')
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [isSendingVerifyCode, setIsSendingVerifyCode] = useState(false)
  const [verifyCountdown, setVerifyCountdown] = useState(0)
  const [message, setMessage] = useState<string | null>(null)
  const [agreementSettings, setAgreementSettings] = useState<LoginAgreementSettings | null>(null)
  const [agreementAccepted, setAgreementAccepted] = useState(false)
  const copy = authCopy[mode]
  const sideCopy = authSideCopy[mode]
  const isRegister = mode === 'register'
  const isLogin = mode === 'login'
  const isReset = mode === 'reset-password'
  const isChange = mode === 'change-password'
  const isResetWithToken = isReset && Boolean(resetEmail && resetToken)
  const requiresAgreement = (isLogin || isRegister) && agreementSettings?.enabled === true
  const rootRef = useRef<HTMLElement>(null)
  const authStyle = {
    '--auth-bg-image': `url("${HERO_BACKGROUND_IMAGE}")`,
  } as CSSProperties
  const emailInputId = 'auth-email'
  const verifyCodeButtonText = verifyCountdown > 0
    ? `${verifyCountdown}s`
    : isSendingVerifyCode
      ? '发送中...'
      : '发送验证码'

  useEffect(() => {
    const root = rootRef.current

    if (
      !root ||
      window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches
    ) {
      return
    }

    const ctx = gsap.context(() => {
      const timeline = gsap.timeline({ defaults: { ease: 'power3.out' } })

      timeline
        .from('.auth-visual', { opacity: 0, x: -18, duration: 0.8 })
        .from('.auth-card', { opacity: 0, y: 18, scale: 0.985, duration: 0.72 }, '-=0.52')
        .from(
          ['.auth-brand', '.auth-visual-copy', '.auth-feature-list'],
          { opacity: 0, y: 12, duration: 0.52, stagger: 0.08 },
          '-=0.58',
        )
        .from(
          ['.auth-header', '.auth-form', '.auth-footer-link', '.auth-secondary-link'],
          { opacity: 0, y: 12, duration: 0.5, stagger: 0.08 },
          '-=0.42',
        )

      gsap.to('.auth-visual', {
        y: -5,
        scale: 1.006,
        duration: 5.2,
        ease: 'sine.inOut',
        repeat: -1,
        yoyo: true,
      })

      gsap.to('.auth-card', {
        y: -4,
        duration: 4.8,
        ease: 'sine.inOut',
        repeat: -1,
        yoyo: true,
      })
    }, root)

    return () => ctx.revert()
  }, [mode])

  useEffect(() => {
    if (!isLogin && !isRegister) {
      setAgreementSettings(null)
      setAgreementAccepted(false)
      return
    }

    let cancelled = false

    fetchLoginAgreementSettings()
      .then((settings) => {
        if (cancelled) {
          return
        }

        setAgreementSettings(settings)
        setAgreementAccepted(!settings.enabled || hasAcceptedLoginAgreement(settings.revision))
      })
      .catch(() => {
        if (cancelled) {
          return
        }

        setAgreementSettings({
          enabled: false,
          mode: 'checkbox',
          updatedAt: '',
          revision: '',
          documents: [],
        })
        setAgreementAccepted(true)
      })

    return () => {
      cancelled = true
    }
  }, [isLogin, isRegister])

  useEffect(() => {
    if (verifyCountdown <= 0) {
      return
    }

    const timer = window.setTimeout(() => {
      setVerifyCountdown((countdown) => Math.max(0, countdown - 1))
    }, 1000)

    return () => window.clearTimeout(timer)
  }, [verifyCountdown])

  useEffect(() => {
    if (!message) {
      return
    }

    const timer = window.setTimeout(() => {
      setMessage(null)
    }, 5000)

    return () => window.clearTimeout(timer)
  }, [message])

  function getCurrentEmail(): string {
    const emailInput = document.getElementById(emailInputId) as HTMLInputElement | null
    return (emailInput?.value || email).trim()
  }

  function isValidEmail(value: string): boolean {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)
  }

  async function handleSendVerifyCode() {
    setMessage(null)
    const currentEmail = getCurrentEmail()

    if (!currentEmail) {
      setMessage('请先填写邮箱。')
      return
    }

    if (!isValidEmail(currentEmail)) {
      setMessage('请输入有效的邮箱地址。')
      return
    }

    setIsSendingVerifyCode(true)

    try {
      const result = await sendRegisterVerifyCode(currentEmail)

      if (!result.ok) {
        setMessage(result.message)
        return
      }

      setVerifyCountdown(Math.max(1, result.countdown))
      setMessage('验证码已发送，请查收邮箱。')
    } finally {
      setIsSendingVerifyCode(false)
    }
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setMessage(null)

    if (isChange) {
      setMessage('请进入控制台个人资料页修改密码。')
      return
    }

    if (requiresAgreement && !agreementAccepted) {
      setMessage('请先阅读并同意最新条款。')
      return
    }

    if (requiresAgreement && agreementAccepted && agreementSettings.revision) {
      persistLoginAgreementAcceptance(agreementSettings.revision)
    }

    setIsSubmitting(true)

    try {
      if (isResetWithToken && newPassword !== confirmPassword) {
        setMessage('两次输入的新密码不一致。')
        return
      }

      const result = isLogin
        ? await loginWithSub2Api(email, password)
        : isRegister
          ? await registerWithSub2Api(email, password, verifyCode, affiliateCode)
          : isResetWithToken
            ? await resetSub2ApiPassword(resetEmail, resetToken, newPassword)
            : await requestPasswordReset(email)

      if (!result.ok) {
        setMessage(result.message)
        return
      }

      if (isReset && !isResetWithToken) {
        setMessage('如果该邮箱已注册，重置说明会发送到你的邮箱。')
        return
      }

      if (isResetWithToken) {
        setMessage('密码已更新，请返回登录。')
        return
      }

      window.location.assign(result.redirectTo)
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <main className="auth-shell" style={authStyle} ref={rootRef}>
      <HeroCurveLines />

      {message && (
        <div className="auth-toast" role="alert" aria-live="assertive">
          <span>{message}</span>
          <button
            className="auth-toast-close"
            type="button"
            aria-label="关闭提示"
            onClick={() => setMessage(null)}
          >
            <X aria-hidden="true" size={15} />
          </button>
        </div>
      )}

      <section className="auth-visual" aria-label="星链账号服务">
        <img className="auth-visual-image" src={HERO_BACKGROUND_IMAGE} alt="" />
        <div className="auth-visual-content auth-reveal">
          <a className="auth-brand" href="/" aria-label={`${brandName} home`}>
            <span className="auth-brand-word">{brandName}</span>
            <span className="auth-brand-submark">API</span>
          </a>

          <div className="auth-visual-copy">
            <p>{sideCopy.eyebrow}</p>
            <h1>{sideCopy.title}</h1>
            <span>{sideCopy.subtitle}</span>
          </div>

          <div className="auth-feature-list" aria-label="服务能力">
            {sideCopy.features.map((feature) => (
              <FeaturePill key={`${mode}-${feature}`} text={feature} />
            ))}
          </div>
        </div>
      </section>

      <section className="auth-panel" aria-labelledby="auth-title">
        <div className="auth-card auth-reveal auth-reveal--delay">
          <span className="fx-border-beam" aria-hidden="true" />
          <div className="auth-mobile-top">
            <a className="auth-back-link" href="/">
              <ArrowLeft aria-hidden="true" size={16} />
              返回首页
            </a>
            <span className="auth-mobile-brand">{brandName}</span>
          </div>

          <div className="auth-header">
            <p>{copy.eyebrow}</p>
            <h2 id="auth-title">{copy.title}</h2>
            <span>{copy.subtitle}</span>
          </div>

          <form className="auth-form" onSubmit={handleSubmit}>
            {!isChange && !isResetWithToken && (
              <InputGroup
                label="邮箱"
                placeholder="you@example.com"
                id={emailInputId}
                type="email"
                autoComplete="email"
                value={email}
                onChange={setEmail}
              />
            )}

            {(isRegister || isLogin) && (
              <InputGroup
                label="密码"
                placeholder="输入密码"
                type={showPassword ? 'text' : 'password'}
                autoComplete={isLogin ? 'current-password' : 'new-password'}
                helper={isRegister ? '至少 8 个字符。' : undefined}
                value={password}
                onChange={setPassword}
                action={
                  <button
                    className="auth-input-action"
                    type="button"
                    aria-label={showPassword ? '隐藏密码' : '显示密码'}
                    onClick={() => setShowPassword((visible) => !visible)}
                  >
                    {showPassword ? (
                      <EyeOff aria-hidden="true" size={17} />
                    ) : (
                      <Eye aria-hidden="true" size={17} />
                    )}
                  </button>
                }
              />
            )}

            {isRegister && (
              <div className="auth-code-row">
                <InputGroup
                  label="邮箱验证码"
                  placeholder="输入验证码"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  value={verifyCode}
                  required={false}
                  onChange={setVerifyCode}
                />
                <button
                  className="auth-code-button"
                  type="button"
                  disabled={isSendingVerifyCode || verifyCountdown > 0}
                  onClick={handleSendVerifyCode}
                >
                  {verifyCodeButtonText}
                </button>
              </div>
            )}

            {isResetWithToken && (
              <>
                <InputGroup
                  label="邮箱"
                  placeholder="you@example.com"
                  type="email"
                  autoComplete="email"
                  value={resetEmail}
                  readOnly
                />
                <InputGroup
                  label="新密码"
                  placeholder="输入新密码"
                  type="password"
                  autoComplete="new-password"
                  helper="至少 8 个字符。"
                  value={newPassword}
                  onChange={setNewPassword}
                />
                <InputGroup
                  label="确认新密码"
                  placeholder="再次输入新密码"
                  type="password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  onChange={setConfirmPassword}
                />
              </>
            )}

            {isChange && (
              <>
                <InputGroup
                  label="当前密码"
                  placeholder="输入当前密码"
                  type="password"
                  autoComplete="current-password"
                  value={currentPassword}
                  onChange={setCurrentPassword}
                />
                <InputGroup
                  label="新密码"
                  placeholder="输入新密码"
                  type="password"
                  autoComplete="new-password"
                  helper="至少 8 个字符。"
                  value={newPassword}
                  onChange={setNewPassword}
                />
                <InputGroup
                  label="确认新密码"
                  placeholder="再次输入新密码"
                  type="password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  onChange={setConfirmPassword}
                />
              </>
            )}

            {requiresAgreement && agreementSettings && (
              <LoginAgreementNotice
                accepted={agreementAccepted}
                settings={agreementSettings}
                onChange={(checked) => {
                  setAgreementAccepted(checked)
                  if (checked) {
                    persistLoginAgreementAcceptance(agreementSettings.revision)
                  }
                }}
              />
            )}

            <button className="auth-submit" type="submit" disabled={isSubmitting}>
              {isSubmitting ? '处理中...' : isResetWithToken ? '重置密码' : copy.button}
            </button>
          </form>

          <p className="auth-footer-link">
            {copy.footerText}{' '}
            <a href={copy.footerHref} aria-label={mode === 'register' ? '已有账号？登录' : undefined}>
              {copy.footerLink}
            </a>
          </p>

          {isLogin && (
            <a className="auth-secondary-link" href="/reset-password">
              找回密码
            </a>
          )}
        </div>
      </section>
    </main>
  )
}

function FeaturePill({ text }: { text: string }) {
  return <span className="auth-feature-pill">{text}</span>
}

function LoginAgreementNotice({
  accepted,
  settings,
  onChange,
}: {
  accepted: boolean
  settings: LoginAgreementSettings
  onChange: (checked: boolean) => void
}) {
  return (
    <div className="auth-agreement">
      <label className="auth-agreement-label">
        <input
          type="checkbox"
          checked={accepted}
          aria-label="同意登录条款"
          onChange={(event) => onChange(event.target.checked)}
        />
        <span>我已阅读并同意</span>
      </label>
      <div className="auth-agreement-links">
        {settings.documents.map((doc, index) => (
          <span key={doc.id || doc.title}>
            <a href={`/legal/${encodeURIComponent(doc.id || doc.title)}`} target="_blank" rel="noreferrer">
              {doc.title}
            </a>
            {index < settings.documents.length - 1 && <span>、</span>}
          </span>
        ))}
      </div>
    </div>
  )
}

function InputGroup({
  label,
  placeholder,
  id,
  type = 'text',
  inputMode,
  autoComplete,
  helper,
  value,
  readOnly,
  required = true,
  action,
  onChange,
}: InputGroupProps) {
  const inputId = id || `auth-${label}`

  return (
    <div className="auth-input-group">
      <label htmlFor={inputId}>{label}</label>
      <span className="auth-input-wrap">
        <input
          id={inputId}
          type={type}
          inputMode={inputMode}
          placeholder={placeholder}
          autoComplete={autoComplete}
          value={value}
          readOnly={readOnly}
          required={required}
          onChange={(event) => onChange?.(event.target.value)}
        />
        {action}
      </span>
      {helper && <small>{helper}</small>}
    </div>
  )
}
