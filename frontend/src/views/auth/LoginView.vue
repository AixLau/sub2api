<template>
  <AuthLayout
    eyebrow="Welcome back"
    title="登录星链"
    subtitle="继续管理你的模型 API 服务、用量与接入配置。"
  >
    <div
      v-if="errorMessage"
      class="mb-6 rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-600"
    >
      {{ errorMessage }}
    </div>

    <form class="auth-form space-y-6" @submit.prevent="handleLogin">
      <div class="space-y-2">
        <label for="email" class="text-sm font-medium text-slate-900">
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
            class="h-12 w-full rounded-xl border border-slate-300 bg-white pl-12 pr-4 text-slate-900"
            :placeholder="t('auth.emailPlaceholder', '请输入邮箱')"
          />
        </div>
        <p v-if="errors.email" class="text-sm text-red-600">
          {{ errors.email }}
        </p>
      </div>

      <div class="space-y-2">
        <label for="password" class="text-sm font-medium text-slate-900">
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
            class="h-12 w-full rounded-xl border border-slate-300 bg-white pl-12 pr-12 text-slate-900"
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
              class="text-slate-400 hover:text-slate-600"
            />
          </button>
        </div>
        <p v-if="errors.password" class="text-sm text-red-600">
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
        <p v-if="errors.turnstile" class="mt-2 text-sm text-red-600">
          {{ errors.turnstile }}
        </p>
      </div>

      <button
        type="submit"
        :disabled="authActionDisabled || (turnstileEnabled && !turnstileToken)"
        class="auth-submit w-full"
      >
        <span v-if="!isLoading">{{ t('auth.signIn', '登录') }}</span>
        <span v-else>{{ t('auth.signingIn', '登录中...') }}</span>
      </button>
    </form>

    <div v-if="showOAuthLogin" class="my-8 flex items-center gap-4">
      <div class="h-px flex-1 bg-slate-200"></div>
      <span class="text-sm text-slate-500">
        {{ t('auth.orContinueWith', '或使用以下方式登录') }}
      </span>
      <div class="h-px flex-1 bg-slate-200"></div>
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

    <router-link
      v-if="passwordResetEnabled && !backendModeEnabled"
      to="/forgot-password"
      class="auth-secondary-link"
    >
      {{ t('auth.forgotPassword', '忘记密码？') }}
    </router-link>

    <template v-if="!backendModeEnabled" #footer>
      {{ t('auth.dontHaveAccount') }}
      <router-link to="/register">{{ t('auth.signUp') }}</router-link>
    </template>
  </AuthLayout>

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
import { AuthLayout } from '@/components/layout'
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
  appStore.showWarning(t('legal.loginAgreementPrompt.loginRejectedWarning'))
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
    appStore.showWarning(t('legal.loginAgreementPrompt.loginRequiredWarning'))
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
