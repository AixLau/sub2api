<template>
  <div class="flex min-h-screen bg-white dark:bg-slate-950">
    <div
      class="relative hidden overflow-hidden bg-gradient-to-br from-blue-600 via-cyan-600 to-blue-700 lg:flex lg:w-1/2"
    >
      <div
        class="absolute left-10 top-20 h-96 w-96 rounded-full bg-white/10 blur-3xl"
      ></div>
      <div
        class="absolute bottom-20 right-10 h-80 w-80 rounded-full bg-cyan-300/20 blur-3xl"
      ></div>

      <div class="relative z-10 flex flex-col justify-center px-16 text-white">
        <h1 class="mb-6 text-5xl font-black leading-tight">
          统一接入<br />
          <span class="drop-shadow-lg">所有 AI 模型</span>
        </h1>
        <p class="mb-12 text-xl text-white/90">
          一个 API 密钥，调用 Claude、GPT、Gemini。<br />
          告别多平台订阅，专注构建应用。
        </p>

        <div class="space-y-4">
          <div class="flex items-center gap-3">
            <div
              class="flex h-10 w-10 items-center justify-center rounded-xl bg-white/20 backdrop-blur-sm ring-1 ring-white/30"
            >
              <Icon name="check" size="md" class="text-white" />
            </div>
            <span class="text-lg text-white">统一 API 密钥管理</span>
          </div>
          <div class="flex items-center gap-3">
            <div
              class="flex h-10 w-10 items-center justify-center rounded-xl bg-white/20 backdrop-blur-sm ring-1 ring-white/30"
            >
              <Icon name="check" size="md" class="text-white" />
            </div>
            <span class="text-lg text-white">极速响应 P50 &lt; 50ms</span>
          </div>
          <div class="flex items-center gap-3">
            <div
              class="flex h-10 w-10 items-center justify-center rounded-xl bg-white/20 backdrop-blur-sm ring-1 ring-white/30"
            >
              <Icon name="check" size="md" class="text-white" />
            </div>
            <span class="text-lg text-white">99.9% 可用性保障</span>
          </div>
        </div>
      </div>
    </div>

    <div class="flex w-full items-center justify-center bg-slate-50 p-6 dark:bg-slate-950 lg:w-1/2">
      <div class="w-full max-w-md">
        <router-link to="/" class="mb-8 flex items-center gap-3 no-underline">
          <div
            class="flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-blue-600 to-cyan-600"
          >
            <span class="text-xl font-bold text-white">AI</span>
          </div>
          <span class="text-xl font-semibold text-slate-900 dark:text-white">{{
            t('common.siteName', '星链 AI Hub')
          }}</span>
        </router-link>

        <div class="mb-8">
          <h2 class="mb-2 text-3xl font-black text-slate-900 dark:text-white">
            {{ t('auth.welcomeBack', '欢迎回来') }}
          </h2>
          <p class="text-slate-600 dark:text-slate-400">
            {{ t('auth.signInToAccount', '登录您的账户以继续') }}
          </p>
        </div>

        <div
          v-if="errorMessage"
          class="mb-6 rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-600 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400"
        >
          {{ errorMessage }}
        </div>

        <form @submit.prevent="handleLogin" class="space-y-6">
          <div class="space-y-2">
            <label for="email" class="text-sm font-medium text-slate-900 dark:text-white">
              {{ t('auth.emailLabel', '邮箱') }}
            </label>
            <div class="relative">
              <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4">
                <Icon name="mail" size="md" class="text-slate-400" />
              </div>
              <input
                id="email"
                v-model="formData.email"
                type="email"
                required
                autofocus
                autocomplete="email"
                :disabled="authActionDisabled"
                class="h-12 w-full rounded-xl border border-slate-300 bg-white pl-12 pr-4 text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                :placeholder="t('auth.emailPlaceholder', '请输入邮箱')"
              />
            </div>
            <p v-if="errors.email" class="text-sm text-red-600 dark:text-red-400">
              {{ errors.email }}
            </p>
          </div>

          <div class="space-y-2">
            <label for="password" class="text-sm font-medium text-slate-900 dark:text-white">
              {{ t('auth.passwordLabel', '密码') }}
            </label>
            <div class="relative">
              <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-4">
                <Icon name="lock" size="md" class="text-slate-400" />
              </div>
              <input
                id="password"
                v-model="formData.password"
                :type="showPassword ? 'text' : 'password'"
                required
                autocomplete="current-password"
                :disabled="authActionDisabled"
                class="h-12 w-full rounded-xl border border-slate-300 bg-white pl-12 pr-12 text-slate-900 dark:border-slate-700 dark:bg-slate-900 dark:text-white"
                :placeholder="t('auth.passwordPlaceholder', '请输入密码')"
              />
              <button
                type="button"
                class="absolute inset-y-0 right-0 flex items-center pr-4"
                :disabled="authActionDisabled"
                @click="showPassword = !showPassword"
              >
                <Icon
                  :name="showPassword ? 'eyeOff' : 'eye'"
                  size="md"
                  class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
                />
              </button>
            </div>
            <p v-if="errors.password" class="text-sm text-red-600 dark:text-red-400">
              {{ errors.password }}
            </p>
          </div>

          <div v-if="turnstileEnabled && publicSettingsLoaded">
            <TurnstileWidget
              ref="turnstileRef"
              :site-key="turnstileSiteKey"
              @verify="onTurnstileVerify"
              @expire="onTurnstileExpire"
              @error="onTurnstileError"
            />
            <p v-if="errors.turnstile" class="mt-2 text-sm text-red-600 dark:text-red-400">
              {{ errors.turnstile }}
            </p>
          </div>

          <button
            type="submit"
            :disabled="authActionDisabled || (turnstileEnabled && !turnstileToken)"
            class="h-12 w-full rounded-xl border-0 bg-gradient-to-r from-blue-600 to-cyan-600 text-base font-semibold text-white hover:from-blue-700 hover:to-cyan-700"
          >
            <span v-if="!isLoading">{{ t('auth.signIn', '登录') }}</span>
            <span v-else>{{ t('auth.signingIn', '登录中...') }}</span>
          </button>
        </form>

        <div v-if="passwordResetEnabled && !backendModeEnabled" class="mt-4 text-center">
          <router-link
            to="/forgot-password"
            class="text-sm text-blue-600 no-underline hover:text-blue-700 dark:text-indigo-400 dark:hover:text-indigo-300"
          >
            {{ t('auth.forgotPassword', '忘记密码？') }}
          </router-link>
        </div>

        <div v-if="showOAuthLogin" class="my-8 flex items-center gap-4">
          <div class="h-px flex-1 bg-slate-200 dark:bg-slate-700"></div>
          <span class="text-sm text-slate-500 dark:text-slate-400">
            {{ t('auth.orContinueWith', '或使用以下方式登录') }}
          </span>
          <div class="h-px flex-1 bg-slate-200 dark:bg-slate-700"></div>
        </div>

        <div v-if="showOAuthLogin" class="space-y-3">
          <EmailOAuthButtons
            v-if="githubOAuthEnabled || googleOAuthEnabled"
            :github-enabled="githubOAuthEnabled"
            :google-enabled="googleOAuthEnabled"
            :disabled="authActionDisabled"
          />

          <LinuxDoOAuthSection v-if="linuxdoOAuthEnabled" :disabled="authActionDisabled" />
          <DingTalkOAuthSection v-if="dingtalkOAuthEnabled" :disabled="authActionDisabled" />
          <WechatOAuthSection v-if="wechatOAuthEnabled" :disabled="authActionDisabled" />
          <OidcOAuthSection
            v-if="oidcOAuthEnabled"
            :provider-name="oidcOAuthProviderName"
            :disabled="authActionDisabled"
          />
        </div>

        <div v-if="!backendModeEnabled" class="mt-8 text-center text-sm text-slate-600 dark:text-slate-400">
          {{ t('auth.dontHaveAccount') }}
          <router-link
            to="/register"
            class="ml-1 font-semibold text-blue-600 no-underline hover:text-blue-700 dark:text-indigo-400 dark:hover:text-indigo-300"
          >
            {{ t('auth.signUp') }}
          </router-link>
        </div>

        <div class="mt-6 text-center">
          <router-link
            to="/"
            class="inline-flex items-center gap-2 text-sm text-slate-500 no-underline hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-300"
          >
            <Icon name="arrowLeft" size="sm" />
            {{ t('auth.backToHome', '返回首页') }}
          </router-link>
        </div>
      </div>
    </div>
  </div>

  <LoginAgreementPrompt
    v-if="loginAgreementEnabled"
    :accepted="agreementAccepted"
    :documents="loginAgreementDocuments"
    :mode="loginAgreementMode"
    :updated-at="loginAgreementUpdatedAt"
    :visible="showAgreementModal"
    @accept="acceptLoginAgreement"
    @reject="rejectLoginAgreement"
    @open="showAgreementModal = true"
  />

  <TotpLoginModal
    v-if="show2FAModal"
    ref="totpModalRef"
    :temp-token="totpTempToken"
    :user-email-masked="totpUserEmailMasked"
    @verify="handle2FAVerify"
    @cancel="handle2FACancel"
  />
</template>

<script setup lang="ts">
import { computed, ref, reactive, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import LinuxDoOAuthSection from '@/components/auth/LinuxDoOAuthSection.vue'
import DingTalkOAuthSection from '@/components/auth/DingTalkOAuthSection.vue'
import OidcOAuthSection from '@/components/auth/OidcOAuthSection.vue'
import WechatOAuthSection from '@/components/auth/WechatOAuthSection.vue'
import EmailOAuthButtons from '@/components/auth/EmailOAuthButtons.vue'
import LoginAgreementPrompt from '@/components/auth/LoginAgreementPrompt.vue'
import TotpLoginModal from '@/components/auth/TotpLoginModal.vue'
import Icon from '@/components/icons/Icon.vue'
import TurnstileWidget from '@/components/TurnstileWidget.vue'
import { useAuthStore, useAppStore } from '@/stores'
import { getPublicSettings, isTotp2FARequired, isWeChatWebOAuthEnabled } from '@/api/auth'
import type { LoginAgreementDocument, TotpLoginResponse } from '@/types'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { clearAllAffiliateReferralCodes } from '@/utils/oauthAffiliate'

const { t } = useI18n()
const LOGIN_AGREEMENT_STORAGE_KEY = 'sub2api_login_agreement_consent'

// ==================== Router & Stores ====================

const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()

// ==================== State ====================

const isLoading = ref<boolean>(false)
const errorMessage = ref<string>('')
const showPassword = ref<boolean>(false)
const publicSettingsLoaded = ref<boolean>(false)

// Public settings
const turnstileEnabled = ref<boolean>(false)
const turnstileSiteKey = ref<string>('')
const linuxdoOAuthEnabled = ref<boolean>(false)
const dingtalkOAuthEnabled = ref<boolean>(false)
const wechatOAuthEnabled = ref<boolean>(false)
const backendModeEnabled = ref<boolean>(false)
const oidcOAuthEnabled = ref<boolean>(false)
const oidcOAuthProviderName = ref<string>('OIDC')
const githubOAuthEnabled = ref<boolean>(false)
const googleOAuthEnabled = ref<boolean>(false)
const passwordResetEnabled = ref<boolean>(false)
const loginAgreementEnabled = ref<boolean>(false)
const loginAgreementMode = ref<'modal' | 'checkbox' | string>('modal')
const loginAgreementUpdatedAt = ref<string>('')
const loginAgreementRevision = ref<string>('')
const loginAgreementDocuments = ref<LoginAgreementDocument[]>([])
const agreementAccepted = ref<boolean>(false)
const showAgreementModal = ref<boolean>(false)

// Turnstile
const turnstileRef = ref<InstanceType<typeof TurnstileWidget> | null>(null)
const turnstileToken = ref<string>('')

// 2FA state
const show2FAModal = ref<boolean>(false)
const totpTempToken = ref<string>('')
const totpUserEmailMasked = ref<string>('')
const totpModalRef = ref<InstanceType<typeof TotpLoginModal> | null>(null)

const formData = reactive({
  email: '',
  password: ''
})

const errors = reactive({
  email: '',
  password: '',
  turnstile: ''
})

const validationToastMessage = computed(
  () => errors.email || errors.password || errors.turnstile || ''
)

const agreementGateActive = computed(
  () => loginAgreementEnabled.value && !agreementAccepted.value
)

const authActionDisabled = computed(
  () => isLoading.value || !publicSettingsLoaded.value || agreementGateActive.value
)

const showOAuthLogin = computed(
  () =>
    !backendModeEnabled.value &&
    (linuxdoOAuthEnabled.value ||
      dingtalkOAuthEnabled.value ||
      wechatOAuthEnabled.value ||
      oidcOAuthEnabled.value ||
      githubOAuthEnabled.value ||
      googleOAuthEnabled.value)
)

watch(validationToastMessage, (value, previousValue) => {
  if (value && value !== previousValue) {
    appStore.showError(value)
  }
})

// ==================== Lifecycle ====================

onMounted(async () => {
  const expiredFlag = sessionStorage.getItem('auth_expired')
  if (expiredFlag) {
    sessionStorage.removeItem('auth_expired')
    const message = t('auth.reloginRequired')
    errorMessage.value = message
    appStore.showWarning(message)
  }

  try {
    const settings = await getPublicSettings()
    turnstileEnabled.value = settings.turnstile_enabled
    turnstileSiteKey.value = settings.turnstile_site_key || ''
    linuxdoOAuthEnabled.value = settings.linuxdo_oauth_enabled
    dingtalkOAuthEnabled.value = settings.dingtalk_oauth_enabled ?? false
    wechatOAuthEnabled.value = isWeChatWebOAuthEnabled(settings)
    backendModeEnabled.value = settings.backend_mode_enabled
    oidcOAuthEnabled.value = settings.oidc_oauth_enabled
    oidcOAuthProviderName.value = settings.oidc_oauth_provider_name || 'OIDC'
    githubOAuthEnabled.value = settings.github_oauth_enabled
    googleOAuthEnabled.value = settings.google_oauth_enabled
    backendModeEnabled.value = settings.backend_mode_enabled
    passwordResetEnabled.value = settings.password_reset_enabled
    applyLoginAgreementSettings(settings)
  } catch (error) {
    console.error('Failed to load public settings:', error)
    loginAgreementEnabled.value = false
    agreementAccepted.value = true
  } finally {
    publicSettingsLoaded.value = true
  }
})

// ==================== Login Agreement ====================

function applyLoginAgreementSettings(settings: {
  login_agreement_enabled?: boolean
  login_agreement_mode?: string
  login_agreement_updated_at?: string
  login_agreement_revision?: string
  login_agreement_documents?: LoginAgreementDocument[]
}): void {
  const documents = Array.isArray(settings.login_agreement_documents)
    ? settings.login_agreement_documents.filter((doc) => doc.title?.trim())
    : []
  loginAgreementDocuments.value = documents
  loginAgreementEnabled.value = settings.login_agreement_enabled === true && documents.length > 0
  loginAgreementMode.value = settings.login_agreement_mode === 'checkbox' ? 'checkbox' : 'modal'
  loginAgreementUpdatedAt.value = settings.login_agreement_updated_at || ''
  loginAgreementRevision.value =
    settings.login_agreement_revision ||
    `${loginAgreementUpdatedAt.value}:${documents.map((doc) => `${doc.id}:${doc.title}`).join('|')}`

  agreementAccepted.value = !loginAgreementEnabled.value || hasAcceptedLoginAgreement(loginAgreementRevision.value)
  showAgreementModal.value =
    loginAgreementEnabled.value && !agreementAccepted.value && loginAgreementMode.value !== 'checkbox'
}

function hasAcceptedLoginAgreement(revision: string): boolean {
  if (!revision) {
    return false
  }
  try {
    const raw = localStorage.getItem(LOGIN_AGREEMENT_STORAGE_KEY)
    if (!raw) {
      return false
    }
    const parsed = JSON.parse(raw) as { revision?: string }
    return parsed.revision === revision
  } catch {
    return false
  }
}

function acceptLoginAgreement(): void {
  if (loginAgreementRevision.value) {
    localStorage.setItem(
      LOGIN_AGREEMENT_STORAGE_KEY,
      JSON.stringify({
        revision: loginAgreementRevision.value,
        accepted_at: new Date().toISOString()
      })
    )
  }
  agreementAccepted.value = true
  showAgreementModal.value = false
}

function rejectLoginAgreement(): void {
  localStorage.removeItem(LOGIN_AGREEMENT_STORAGE_KEY)
  agreementAccepted.value = false
  showAgreementModal.value = false
  appStore.showWarning('未同意最新条款前，无法输入账号密码或使用快捷登录。')
}

// ==================== Turnstile Handlers ====================

function onTurnstileVerify(token: string): void {
  turnstileToken.value = token
  errors.turnstile = ''
}

function onTurnstileExpire(): void {
  turnstileToken.value = ''
  errors.turnstile = t('auth.turnstileExpired')
}

function onTurnstileError(): void {
  turnstileToken.value = ''
  errors.turnstile = t('auth.turnstileFailed')
}

// ==================== Validation ====================

function validateForm(): boolean {
  // Reset errors
  errors.email = ''
  errors.password = ''
  errors.turnstile = ''

  let isValid = true

  if (agreementGateActive.value) {
    appStore.showWarning('请先阅读并同意最新条款后再登录。')
    if (loginAgreementMode.value !== 'checkbox') {
      showAgreementModal.value = true
    }
    return false
  }

  // Email validation
  if (!formData.email.trim()) {
    errors.email = t('auth.emailRequired')
    isValid = false
  } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(formData.email)) {
    errors.email = t('auth.invalidEmail')
    isValid = false
  }

  // Password validation
  if (!formData.password) {
    errors.password = t('auth.passwordRequired')
    isValid = false
  } else if (formData.password.length < 6) {
    errors.password = t('auth.passwordMinLength')
    isValid = false
  }

  // Turnstile validation
  if (turnstileEnabled.value && !turnstileToken.value) {
    errors.turnstile = t('auth.completeVerification')
    isValid = false
  }

  return isValid
}

// ==================== Form Handlers ====================

async function handleLogin(): Promise<void> {
  // Clear previous error
  errorMessage.value = ''

  // Validate form
  if (!validateForm()) {
    return
  }

  isLoading.value = true

  try {
    // Call auth store login
    const response = await authStore.login({
      email: formData.email,
      password: formData.password,
      turnstile_token: turnstileEnabled.value ? turnstileToken.value : undefined
    })

    // Check if 2FA is required
    if (isTotp2FARequired(response)) {
      const totpResponse = response as TotpLoginResponse
      totpTempToken.value = totpResponse.temp_token || ''
      totpUserEmailMasked.value = totpResponse.user_email_masked || ''
      show2FAModal.value = true
      isLoading.value = false
      return
    }

    // Show success toast
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))

    // Redirect to dashboard or intended route
    const redirectTo = (router.currentRoute.value.query.redirect as string) || '/dashboard'
    await router.push(redirectTo)
  } catch (error: unknown) {
    // Reset Turnstile on error
    if (turnstileRef.value) {
      turnstileRef.value.reset()
      turnstileToken.value = ''
    }

    errorMessage.value = extractI18nErrorMessage(error, t, 'auth.errors', t('auth.loginFailed'))

    // Also show error toast
    appStore.showError(errorMessage.value)
  } finally {
    isLoading.value = false
  }
}

// ==================== 2FA Handlers ====================

async function handle2FAVerify(code: string): Promise<void> {
  if (totpModalRef.value) {
    totpModalRef.value.setVerifying(true)
  }

  try {
    await authStore.login2FA(totpTempToken.value, code)

    // Close modal and show success
    show2FAModal.value = false
    clearAllAffiliateReferralCodes()
    appStore.showSuccess(t('auth.loginSuccess'))

    // Redirect to dashboard or intended route
    const redirectTo = (router.currentRoute.value.query.redirect as string) || '/dashboard'
    await router.push(redirectTo)
  } catch (error: unknown) {
    const err = error as { message?: string; response?: { data?: { message?: string } } }
    const message = err.response?.data?.message || err.message || t('profile.totp.loginFailed')

    if (totpModalRef.value) {
      totpModalRef.value.setError(message)
      totpModalRef.value.setVerifying(false)
    }
  }
}

function handle2FACancel(): void {
  show2FAModal.value = false
  totpTempToken.value = ''
  totpUserEmailMasked.value = ''
}
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
