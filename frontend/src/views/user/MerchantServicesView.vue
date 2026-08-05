<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <div>
        <h1 class="text-2xl font-semibold text-content-primary">{{ t('merchant.title') }}</h1>
        <p class="mt-1 text-sm text-content-secondary">{{ t('merchant.description') }}</p>
      </div>

      <div v-if="loading" class="rounded-lg border border-line-subtle bg-surface-raised p-8 text-center text-sm text-content-secondary">
        {{ t('common.loading') }}
      </div>
      <div v-else-if="integrations.length === 0" class="rounded-lg border border-dashed border-line-subtle bg-surface-raised p-10 text-center">
        <Icon name="globe" size="xl" class="mx-auto text-content-tertiary" />
        <p class="mt-4 text-sm text-content-secondary">{{ t('merchant.empty') }}</p>
      </div>
      <div v-else class="grid gap-4 md:grid-cols-2">
        <article v-for="item in integrations" :key="item.id" class="flex min-h-44 flex-col justify-between rounded-lg border border-line-subtle bg-surface-raised p-5">
          <div>
            <div class="flex items-start justify-between gap-3">
              <div class="flex min-w-0 items-center gap-3">
                <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary-50 text-primary-600 dark:bg-primary-900/20 dark:text-primary-300">
                  <Icon name="globe" size="lg" />
                </span>
                <div class="min-w-0">
                  <h2 class="truncate text-base font-semibold text-content-primary">{{ item.name }}</h2>
                  <p class="truncate font-mono text-xs text-content-tertiary">{{ item.code }}</p>
                </div>
              </div>
            </div>
            <p v-if="item.description" class="mt-4 text-sm leading-6 text-content-secondary">{{ item.description }}</p>
          </div>
          <div class="mt-5 flex items-center justify-between gap-3">
            <span class="text-xs text-content-tertiary">{{ t('merchant.openInNewWindow') }}</span>
            <button class="btn btn-primary shrink-0" type="button" :disabled="launchingId === item.id" @click="launch(item.id)">
              <Icon name="externalLink" size="md" class="mr-2" />
              {{ launchingId === item.id ? t('merchant.opening') : t('merchant.open') }}
            </button>
          </div>
        </article>
      </div>

      <section v-if="bindings.length" class="rounded-lg border border-line-subtle bg-surface-raised p-5">
        <div class="mb-4 flex items-center gap-2">
          <Icon name="link" size="md" class="text-content-tertiary" />
          <h2 class="text-base font-semibold text-content-primary">{{ t('merchant.bindingsTitle') }}</h2>
        </div>
        <div class="divide-y divide-line-subtle">
          <div v-for="binding in bindings" :key="binding.id" class="grid gap-3 py-3 text-sm sm:grid-cols-[1.2fr_1fr_1fr_auto] sm:items-center">
            <div>
              <div class="font-medium text-content-primary">{{ binding.integration_name || binding.integration_code }}</div>
              <div class="mt-1 text-xs text-content-tertiary">{{ t('merchant.account') }}: {{ binding.external_account || binding.external_user_id }}</div>
            </div>
            <div>
              <div class="text-xs text-content-tertiary">{{ t('merchant.externalUserId') }}</div>
              <code class="text-xs text-content-secondary">{{ binding.external_user_id }}</code>
            </div>
            <div class="sm:text-right">
              <div class="text-xs text-content-tertiary">{{ t('merchant.lastLogin') }}</div>
              <div class="text-sm text-content-secondary">{{ formatDate(binding.last_login_at) }}</div>
            </div>
            <div class="flex flex-wrap items-center gap-2 sm:justify-end">
              <button
                class="btn btn-secondary btn-sm"
                type="button"
                :disabled="bindingActionKey !== null"
                @click="runBindingAction(binding, 'sync')"
              >
                <Icon name="sync" size="sm" class="mr-1.5" :class="{ 'animate-spin': bindingActionKey === actionKey(binding.id, 'sync') }" />
                {{ bindingActionKey === actionKey(binding.id, 'sync') ? t('merchant.syncing') : t('merchant.sync') }}
              </button>
              <button
                class="btn btn-secondary btn-sm"
                type="button"
                :disabled="bindingActionKey !== null"
                @click="runBindingAction(binding, 'bind')"
              >
                <Icon name="link" size="sm" class="mr-1.5" :class="{ 'animate-spin': bindingActionKey === actionKey(binding.id, 'bind') }" />
                {{ bindingActionKey === actionKey(binding.id, 'bind') ? t('merchant.binding') : t('merchant.bind') }}
              </button>
              <button
                class="btn btn-secondary btn-sm"
                type="button"
                :disabled="bindingActionKey !== null"
                @click="runBindingAction(binding, 'status')"
              >
                <Icon name="refresh" size="sm" class="mr-1.5" :class="{ 'animate-spin': bindingActionKey === actionKey(binding.id, 'status') }" />
                {{ bindingActionKey === actionKey(binding.id, 'status') ? t('merchant.checking') : t('merchant.checkStatus') }}
              </button>
            </div>
            <p
              v-if="bindingFeedback[binding.id]"
              class="sm:col-start-4 text-xs sm:text-right"
              :class="bindingFeedback[binding.id].kind === 'success' ? 'text-status-success' : 'text-status-danger'"
            >
              {{ bindingFeedback[binding.id].message }}
            </p>
          </div>
        </div>
      </section>
      <p v-else-if="!loading" class="text-xs text-content-tertiary">{{ t('merchant.noBinding') }}</p>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { userAPI, type UserMerchantBinding, type UserMerchantIntegration } from '@/api/user'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const integrations = ref<UserMerchantIntegration[]>([])
const bindings = ref<UserMerchantBinding[]>([])
const loading = ref(false)
const launchingId = ref<number | null>(null)
const bindingActionKey = ref<string | null>(null)
const bindingFeedback = reactive<Record<number, { kind: 'success' | 'error'; message: string }>>({})

function formatDate(value?: string): string {
  if (!value) return t('merchant.never')
  try {
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  } catch {
    return value
  }
}

async function load() {
  loading.value = true
  try {
    const [available, connected] = await Promise.all([
      userAPI.listMerchantIntegrations(),
      userAPI.listMerchantBindings().catch(() => [] as UserMerchantBinding[])
    ])
    integrations.value = available
    bindings.value = connected
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('merchant.launchFailed')))
  } finally {
    loading.value = false
  }
}

async function launch(integrationId: number) {
  const popup = window.open('', '_blank')
  if (!popup) {
    appStore.showError(t('merchant.launchFailed'))
    return
  }
  popup.opener = null
  launchingId.value = integrationId
  try {
    const result = await userAPI.launchMerchantIntegration(integrationId)
    if (!result.redirect_url) throw new Error('missing redirect')
    popup.location.href = result.redirect_url
  } catch (error: unknown) {
    popup.close()
    appStore.showError(extractApiErrorMessage(error, t('merchant.launchFailed')))
  } finally {
    launchingId.value = null
  }
}

type BindingAction = 'sync' | 'bind' | 'status'

function actionKey(bindingId: number, action: BindingAction): string {
  return `${bindingId}:${action}`
}

async function runBindingAction(binding: UserMerchantBinding, action: BindingAction) {
  const key = actionKey(binding.id, action)
  bindingActionKey.value = key
  delete bindingFeedback[binding.id]
  try {
    if (action === 'sync') {
      await userAPI.syncMerchantBinding(binding.id)
    } else if (action === 'bind') {
      await userAPI.bindMerchantBinding(binding.id)
    } else {
      await userAPI.refreshMerchantBindingStatus(binding.id)
    }
    bindingFeedback[binding.id] = { kind: 'success', message: t(`merchant.${action}Success`) }
    bindings.value = await userAPI.listMerchantBindings()
  } catch (error: unknown) {
    bindingFeedback[binding.id] = {
      kind: 'error',
      message: extractApiErrorMessage(error, t(`merchant.${action}Failed`))
    }
  } finally {
    bindingActionKey.value = null
  }
}

onMounted(load)
</script>
