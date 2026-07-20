<template>
  <AppLayout>
    <div data-testid="recharge-liquid-page" class="recharge-page-canvas">
      <div class="recharge-page-content mx-auto max-w-6xl space-y-5">
        <div v-if="loading" class="flex items-center justify-center py-20">
          <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
        </div>

        <template v-else>
          <PurchaseModeTabs
            v-if="paymentPhase === 'select'"
            v-model="activeTab"
            :tabs="tabs"
          />

          <template v-if="paymentPhase === 'paying'">
            <PaymentStatusPanel
              :order-id="paymentState.orderId"
              :qr-code="paymentState.qrCode"
              :expires-at="paymentState.expiresAt"
              :payment-type="paymentState.paymentType"
              :pay-url="paymentState.payUrl"
              :order-type="paymentState.orderType"
              :currency="paymentState.currency || selectedCurrency"
              :pay-amount="paymentState.payAmount || paymentState.amount"
              @done="onPaymentDone"
              @success="onPaymentSuccess"
              @settled="onPaymentSettled"
            />
          </template>

          <template v-else>
            <template v-if="activeTab === 'recharge'">
              <div v-if="enabledMethods.length === 0" class="recharge-glass-card py-16 text-center">
                <p class="text-gray-500 dark:text-gray-400">{{ t('payment.notAvailable') }}</p>
              </div>

              <template v-if="enabledMethods.length > 0">
                <div class="space-y-5">
                  <AccountBalanceHero
                    :account-name="accountDisplayName"
                    :formatted-balance="formattedCurrentBalance"
                  />

                  <div
                    v-if="!isNinePlusSelected"
                    data-testid="recharge-layout"
                    class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] xl:items-start"
                  >
                    <div data-testid="recharge-controls" class="space-y-5">
                      <RechargeAmountSelector
                        v-model="amount"
                        :amounts="quickRechargeAmounts"
                        :min="effectiveMinAmount"
                        :max="effectiveMaxAmount"
                        :currency="selectedCurrency"
                        :locale="localeCode"
                        :error="amountError"
                        :format-amount="formatSelectedPaymentAmount"
                        :show-header="false"
                        :show-preset-meta="false"
                      />
                      <RechargeMethodSelector
                        v-if="rechargeMethodTypes.length >= 1"
                        :methods="methodOptions"
                        :selected="selectedMethod"
                        :show-header="false"
                        @select="selectedMethod = $event"
                      />
                      <RechargeTrustBar class="hidden xl:block" />
                    </div>
                    <RechargeOrderSummary
                      :formatted-amount="formatSelectedPaymentAmount(validAmount)"
                      :formatted-fee="formatSelectedPaymentAmount(feeAmount)"
                      :formatted-total="formatSelectedPaymentAmount(totalAmount)"
                      :formatted-estimated-credited-amount="formattedEstimatedCreditedAmount"
                      :disabled="!canSubmit || submitting"
                      :submitting="submitting"
                      :has-submitted="submitAttempted"
                      :error-message="errorMessage"
                      :error-hint-message="errorHintMessage"
                      @submit="handleSubmitRecharge"
                    />
                  </div>

                  <div
                    v-else
                    data-testid="recharge-layout"
                    class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px] lg:items-start"
                  >
                    <div data-testid="recharge-controls" class="space-y-5">
                      <div class="recharge-glass-card p-5 sm:p-6">
                        <div class="mb-3">
                          <p class="recharge-section-title">{{ t('payment.nineplus.selectProduct') }}</p>
                        </div>
                        <div v-if="availableNinePlusProducts.length" class="grid gap-3 sm:grid-cols-2">
                          <button
                            v-for="product in availableNinePlusProducts"
                            :key="product.product_id"
                            type="button"
                            :data-testid="`nineplus-product-${product.product_id}`"
                            class="recharge-choice-card px-4 py-3 text-left"
                            :class="selectedNinePlusProductId === product.product_id
                              ? 'recharge-choice-card-selected'
                              : ''"
                            @click="selectedNinePlusProductId = product.product_id"
                          >
                            <div class="flex items-start justify-between gap-3">
                              <span class="text-sm font-medium text-slate-600">{{ product.display_name }}</span>
                              <span v-if="product.badge" class="rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-medium text-blue-700">{{ product.badge }}</span>
                            </div>
                            <div class="mt-2 flex flex-wrap items-end justify-between gap-2">
                              <p class="text-2xl font-semibold tracking-normal text-slate-950">{{ formatNinePlusCreditedAmount(product) || product.display_name }}</p>
                              <p class="text-sm font-semibold text-blue-700">
                                {{ t('payment.amountLabel') }} {{ formatSelectedPaymentAmount(ninePlusProductPriceAmount(product)) }}
                              </p>
                            </div>
                          </button>
                        </div>
                        <p v-else class="py-8 text-center text-sm text-slate-500">{{ t('payment.nineplus.noProducts') }}</p>
                      </div>
                      <RechargeMethodSelector
                        v-if="rechargeMethodTypes.length >= 1"
                        :methods="methodOptions"
                        :selected="selectedMethod"
                        :show-header="false"
                        @select="selectedMethod = $event"
                      />
                      <RechargeTrustBar class="hidden lg:block" />
                    </div>
                    <aside data-testid="recharge-confirmation" class="recharge-glass-card p-5 sm:p-6 lg:sticky lg:top-24">
                      <div class="space-y-4">
                        <div>
                          <p class="recharge-section-title">{{ t('payment.rechargeUi.orderSummary') }}</p>
                          <p class="mt-1 text-3xl font-semibold tracking-normal text-slate-950">{{ formatSelectedPaymentAmount(totalAmount) }}</p>
                        </div>
                        <div v-if="validAmount > 0" class="space-y-2 text-sm">
                          <div v-if="isNinePlusSelected && selectedNinePlusCreditedAmountLabel" class="flex justify-between">
                            <span class="text-slate-500">{{ t('payment.creditedBalance') }}</span>
                            <span class="font-semibold text-slate-950">{{ selectedNinePlusCreditedAmountLabel }}</span>
                          </div>
                          <div class="flex justify-between">
                            <span class="text-slate-500">{{ isNinePlusSelected ? t('payment.amountLabel') : t('payment.paymentAmount') }}</span>
                            <span class="text-slate-950">{{ formatSelectedPaymentAmount(isNinePlusSelected ? selectedNinePlusProductPriceAmount : validAmount) }}</span>
                          </div>
                          <div v-if="isNinePlusSelected && selectedNinePlusProductFeeAmount > 0" class="flex justify-between">
                            <span class="text-slate-500">{{ t('payment.fee') }}</span>
                            <span class="text-slate-950">{{ formatSelectedPaymentAmount(selectedNinePlusProductFeeAmount) }}</span>
                          </div>
                          <div v-else-if="feeRate > 0" class="flex justify-between">
                            <span class="text-slate-500">{{ t('payment.fee') }} ({{ feeRate }}%)</span>
                            <span class="text-slate-950">{{ formatSelectedPaymentAmount(feeAmount) }}</span>
                          </div>
                          <div
                            v-if="balanceRechargeMultiplier !== 1 && !isNinePlusSelected"
                            class="flex justify-between"
                            :class="{ 'border-t border-gray-200 pt-2 dark:border-dark-600': feeRate <= 0 }"
                          >
                            <span class="text-gray-500 dark:text-gray-400">{{ t('payment.creditedBalance') }}</span>
                            <span class="text-gray-900 dark:text-white">${{ creditedAmount.toFixed(2) }}</span>
                          </div>
                          <p
                            v-if="balanceRechargeMultiplier !== 1 && !isNinePlusSelected"
                            class="border-t border-gray-200 pt-2 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400"
                          >
                            {{ t('payment.rechargeRatePreview', { usd: balanceRechargeMultiplier.toFixed(2) }) }}
                          </p>
                        </div>
                        <div v-if="errorMessage" class="rounded-2xl border border-red-200 bg-red-50/80 px-4 py-3 text-sm text-red-700" role="alert">
                          <p>{{ errorMessage }}</p>
                          <p v-if="errorHintMessage" class="mt-1 text-red-600/80">{{ errorHintMessage }}</p>
                        </div>
                        <button data-testid="submit-recharge" class="recharge-primary-button w-full" :disabled="!canSubmit || submitting" :aria-busy="submitting" @click="handleSubmitRecharge">
                          <span v-if="submitting" class="flex items-center justify-center gap-2">
                            <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                            {{ t('common.processing') }}
                          </span>
                          <span v-else>{{ t('payment.rechargeUi.rechargeNow') }}</span>
                        </button>
                      </div>
                    </aside>
                  </div>

                  <RechargeTrustBar :class="isNinePlusSelected ? 'lg:hidden' : 'xl:hidden'" />
                </div>
              </template>
            </template>

            <template v-else-if="activeTab === 'subscription' && ninePlusSubscriptionProducts.length > 0">
              <div data-testid="subscription-layout" class="space-y-5">
                <CurrentSubscriptionCard
                  v-if="currentSubscriptionSummary"
                  :subscription="currentSubscriptionSummary"
                />
                <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] xl:items-start">
                  <div data-testid="subscription-controls" class="space-y-5">
                    <section class="recharge-glass-card p-5 sm:p-6" aria-labelledby="nineplus-subscription-title">
                      <div class="mb-4">
                        <p id="nineplus-subscription-title" class="recharge-section-title">{{ t('payment.nineplus.selectSubscriptionProduct') }}</p>
                      </div>
                      <div class="grid gap-3 sm:grid-cols-2">
                        <button
                          v-for="product in ninePlusSubscriptionProducts"
                          :key="product.product_id"
                          type="button"
                          :data-testid="`nineplus-subscription-product-${product.product_id}`"
                          class="recharge-choice-card px-4 py-4 text-left"
                          :class="{ 'recharge-choice-card-selected': selectedNinePlusSubscriptionProductId === product.product_id }"
                          @click="selectedNinePlusSubscriptionProductId = product.product_id"
                        >
                          <div class="flex items-start justify-between gap-3">
                            <span class="min-w-0">
                              <span class="block break-words text-base font-semibold text-slate-950">{{ ninePlusSubscriptionProductTitle(product) }}</span>
                              <span v-if="product.description" class="mt-1 block line-clamp-2 text-xs leading-relaxed text-slate-500">{{ product.description }}</span>
                            </span>
                            <span v-if="ninePlusProductCategory(product)" class="shrink-0 rounded-full bg-blue-100 px-2 py-0.5 text-[10px] font-semibold text-blue-700">{{ ninePlusProductCategory(product) }}</span>
                          </div>
                          <div class="mt-4 flex flex-wrap items-end justify-between gap-3">
                            <p class="text-2xl font-semibold tracking-normal text-blue-700">{{ formatNinePlusCreditedAmount(product) }}</p>
                            <p class="text-xs font-semibold text-slate-500">
                              {{ t('payment.packagePriceShort') }} {{ formatSelectedPaymentAmount(ninePlusProductPriceAmount(product)) }}
                            </p>
                          </div>
                        </button>
                      </div>
                    </section>
                    <RechargeMethodSelector
                      v-if="subscriptionNinePlusMethodOptions.length"
                      :methods="subscriptionNinePlusMethodOptions"
                      :selected="selectedMethod"
                      :show-header="false"
                      @select="selectedMethod = $event"
                    />
                  </div>
                  <aside data-testid="subscription-confirmation" class="recharge-glass-card recharge-summary-card p-5 sm:p-6">
                    <p class="recharge-section-title">{{ t('payment.rechargeUi.orderSummary') }}</p>
                    <div class="mt-5 space-y-4 text-sm">
                      <div v-if="effectiveNinePlusSubscriptionProduct" class="flex items-center justify-between gap-4">
                        <span class="text-slate-500">{{ t('payment.nineplus.selectedSubscription') }}</span>
                        <span class="min-w-0 max-w-[190px] break-words text-right font-semibold text-slate-950">{{ ninePlusSubscriptionProductTitle(effectiveNinePlusSubscriptionProduct) }}</span>
                      </div>
                      <div v-if="selectedNinePlusSubscriptionQuotaLabel" class="flex items-center justify-between gap-4">
                        <span class="text-slate-500">{{ t('payment.planCard.quota') }}</span>
                        <span class="font-semibold text-slate-950">{{ selectedNinePlusSubscriptionQuotaLabel }}</span>
                      </div>
                      <div class="flex items-center justify-between gap-4">
                        <span class="text-slate-500">{{ t('payment.packagePrice') }}</span>
                        <span class="font-semibold text-slate-950">{{ formatSelectedPaymentAmount(selectedNinePlusSubscriptionProductPriceAmount) }}</span>
                      </div>
                      <div v-if="selectedNinePlusSubscriptionProductFeeAmount > 0" class="flex items-center justify-between gap-4">
                        <span class="text-slate-500">{{ t('payment.fee') }}</span>
                        <span class="font-semibold text-slate-950">{{ formatSelectedPaymentAmount(selectedNinePlusSubscriptionProductFeeAmount) }}</span>
                      </div>
                      <div class="border-t border-slate-200/70 pt-4">
                        <div class="flex items-center justify-between gap-4">
                          <span class="font-semibold text-slate-950">{{ t('payment.payableAmount') }}</span>
                          <span class="text-2xl font-semibold text-blue-700">{{ formatSelectedPaymentAmount(ninePlusSubscriptionAmount) }}</span>
                        </div>
                      </div>
                    </div>
                    <button data-testid="submit-subscription" class="recharge-primary-button mt-6 w-full" :disabled="!canSubmitNinePlusSubscription || submitting" :aria-busy="submitting" @click="handleSubmitNinePlusSubscription">
                      <span v-if="submitting" class="flex items-center justify-center gap-2">
                        <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                        {{ t('common.processing') }}
                      </span>
                      <span v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(ninePlusSubscriptionAmount) }}</span>
                    </button>
                  </aside>
                </div>
                <RechargeTrustBar />
              </div>
            </template>

            <template v-else-if="activeTab === 'subscription'">
              <div data-testid="subscription-layout" class="space-y-5">
                <CurrentSubscriptionCard
                  v-if="currentSubscriptionSummary"
                  :subscription="currentSubscriptionSummary"
                />
                <!-- Subscription confirm (inline, replaces plan list) -->
                <template v-if="selectedPlan">
                  <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px] xl:items-start">
                    <div class="space-y-5">
                      <section class="recharge-glass-card p-5 sm:p-6" aria-labelledby="selected-plan-title">
                        <div>
                          <div class="min-w-0">
                            <h3 id="selected-plan-title" class="break-words text-lg font-extrabold leading-tight text-slate-950">{{ selectedPlan.name }}</h3>
                            <p v-if="selectedPlanDisplay.description" class="mt-2 line-clamp-2 text-sm leading-relaxed text-slate-500">
                              {{ selectedPlanDisplay.description }}
                            </p>
                          </div>
                        </div>
                        <div class="mt-6 flex flex-wrap items-end justify-between gap-3">
                          <div v-if="selectedPlanDisplay.quotaSummary || selectedPlanDisplay.validitySummary" class="min-w-0">
                            <p v-if="selectedPlanDisplay.quotaSummary" class="text-2xl font-extrabold tracking-normal text-slate-950">
                              {{ selectedPlanDisplay.quotaSummary }}
                            </p>
                            <p v-if="selectedPlanDisplay.validitySummary" class="mt-1 text-sm font-semibold text-slate-500">
                              {{ selectedPlanDisplay.validitySummary }}
                            </p>
                          </div>
                          <div class="min-w-0">
                            <div>
                              <span class="text-3xl font-extrabold tracking-normal text-blue-700">{{ formatSelectedSubscriptionPaymentAmount(selectedPlan.price) }}</span>
                            </div>
                            <div v-if="selectedPlan.original_price" class="mt-1">
                              <span class="text-sm text-slate-400 line-through">
                                {{ formatSelectedSubscriptionPaymentAmount(selectedPlan.original_price) }}
                              </span>
                            </div>
                          </div>
                        </div>
                      </section>
                      <RechargeMethodSelector
                        v-if="subMethodOptions.length"
                        :methods="subMethodOptions"
                        :selected="selectedMethod"
                        :show-header="false"
                        @select="selectedMethod = $event"
                      />
                    </div>
                    <aside data-testid="subscription-confirmation" class="recharge-glass-card recharge-summary-card p-5 sm:p-6">
                      <p class="recharge-section-title">{{ t('payment.rechargeUi.orderSummary') }}</p>
                      <div class="mt-5 space-y-4 text-sm">
                        <div class="flex items-center justify-between gap-4">
                          <span class="text-slate-500">{{ t('payment.rechargeUi.selectedPlan') }}</span>
                          <span class="min-w-0 max-w-[190px] break-words text-right font-semibold text-slate-950">{{ selectedPlan.name }}</span>
                        </div>
                        <div class="flex items-center justify-between gap-4">
                          <span class="text-slate-500">{{ t('payment.amountLabel') }}</span>
                          <span class="font-semibold text-slate-950">{{ formatSelectedPaymentAmount(subPaymentAmount) }}</span>
                        </div>
                        <div v-if="feeRate > 0 && selectedPlan.price > 0" class="flex items-center justify-between gap-4">
                          <span class="text-slate-500">{{ t('payment.fee') }} ({{ feeRate }}%)</span>
                          <span class="font-semibold text-slate-950">{{ formatSelectedPaymentAmount(subFeeAmount) }}</span>
                        </div>
                        <div class="border-t border-slate-200/70 pt-4">
                          <div class="flex items-center justify-between gap-4">
                            <span class="font-semibold text-slate-950">{{ t('payment.actualPay') }}</span>
                            <span class="text-2xl font-semibold text-blue-700">{{ formatSelectedPaymentAmount(subTotalAmount) }}</span>
                          </div>
                        </div>
                      </div>
                      <button data-testid="submit-subscription" class="recharge-primary-button mt-6 w-full" :disabled="!canSubmitSubscription || submitting" :aria-busy="submitting" @click="confirmSubscribe">
                        <span v-if="submitting" class="flex items-center justify-center gap-2">
                          <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                          {{ t('common.processing') }}
                        </span>
                        <span v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(subTotalAmount) }}</span>
                      </button>
                      <button class="recharge-secondary-button mt-3 w-full" @click="selectedPlan = null">{{ t('common.cancel') }}</button>
                    </aside>
                  </div>
                </template>
                <!-- Plan list -->
                <template v-else>
                  <section v-if="checkout.plans.length === 0" class="recharge-glass-card py-16 text-center">
                    <Icon name="gift" size="xl" class="mx-auto mb-3 text-slate-300" />
                    <p class="text-slate-500">{{ t('payment.noPlans') }}</p>
                  </section>
                  <section v-else class="recharge-glass-card p-5 sm:p-6" aria-labelledby="subscription-plans-title">
                    <div class="mb-4">
                      <p id="subscription-plans-title" class="recharge-section-title">{{ t('payment.tabSubscribe') }}</p>
                    </div>
                    <div :class="planGridClass">
                      <SubscriptionPlanCard v-for="plan in checkout.plans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlan" />
                    </div>
                  </section>
                </template>
                <RechargeTrustBar />
              </div>
            </template>
          </template>

          <div v-if="(checkout.help_text || checkout.help_image_url) && paymentPhase === 'select' && !selectedPlan" class="recharge-glass-card p-4">
            <div class="flex flex-col items-center gap-3">
              <img
                v-if="checkout.help_image_url"
                :src="checkout.help_image_url"
                alt=""
                class="h-40 max-w-full cursor-pointer rounded-lg object-contain transition-opacity hover:opacity-80"
                @click="previewImage = checkout.help_image_url"
              />
              <p v-if="checkout.help_text" class="text-center text-sm text-slate-500">{{ checkout.help_text }}</p>
            </div>
          </div>
        </template>
      </div>
    </div>
    <!-- Renewal Plan Selection Modal -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="showRenewalModal" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="closeRenewalModal">
          <div class="relative w-full max-w-lg rounded-2xl border border-gray-200 bg-white p-6 shadow-2xl dark:border-dark-700 dark:bg-dark-900">
            <!-- Close button -->
            <button class="absolute right-4 top-4 rounded-lg p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-200" @click="closeRenewalModal">
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
            <h3 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.selectPlan') }}</h3>
            <div class="space-y-4">
              <SubscriptionPlanCard v-for="plan in renewalPlans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlanFromModal" />
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
    <!-- Image Preview Overlay -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="previewImage" class="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 backdrop-blur-sm" @click="previewImage = ''">
          <img :src="previewImage" alt="" class="max-h-[85vh] max-w-[90vw] rounded-xl object-contain shadow-2xl" />
        </div>
      </Transition>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { usePaymentStore } from '@/stores/payment'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'
import type {
  SubscriptionPlan,
  CheckoutInfoResponse,
  CreateOrderResult,
  NinePlusProduct,
  OrderType,
} from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import AccountBalanceHero from '@/components/payment/recharge/AccountBalanceHero.vue'
import CurrentSubscriptionCard from '@/components/payment/recharge/CurrentSubscriptionCard.vue'
import RechargeAmountSelector from '@/components/payment/recharge/RechargeAmountSelector.vue'
import RechargeMethodSelector from '@/components/payment/recharge/RechargeMethodSelector.vue'
import RechargeOrderSummary from '@/components/payment/recharge/RechargeOrderSummary.vue'
import RechargeTrustBar from '@/components/payment/recharge/RechargeTrustBar.vue'
import PurchaseModeTabs from '@/components/payment/recharge/PurchaseModeTabs.vue'
import { METHOD_ORDER, getPaymentPopupFeatures } from '@/components/payment/providerConfig'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  buildCreateOrderPayload,
  clearPaymentRecoverySnapshot,
  decidePaymentLaunch,
  getVisibleMethods,
  normalizeVisibleMethod,
  readPaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import { platformLabel } from '@/utils/platformColors'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import Icon from '@/components/icons/Icon.vue'
import { DEFAULT_PAYMENT_CURRENCY, formatPaymentAmount, normalizePaymentCurrency } from '@/components/payment/currency'
import { buildSubscriptionPlanDisplay, buildSubscriptionPlanDisplayLabels, type SubscriptionPlanDisplay } from '@/components/payment/subscriptionPlanDisplay'
import type { PaymentMethodOption } from '@/components/payment/PaymentMethodSelector.vue'
import { buildPaymentErrorToastMessage, describePaymentScenarioError } from './paymentUx'
import { hasWechatResumeQuery, parseWechatResumeRoute, stripWechatResumeQuery } from './paymentWechatResume'
import '@/components/payment/recharge/recharge-liquid.css'

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const paymentStore = usePaymentStore()
const subscriptionStore = useSubscriptionStore()
const appStore = useAppStore()

const user = computed(() => authStore.user)
const activeSubscriptions = computed(() => subscriptionStore.activeSubscriptions)

function getDaysRemaining(expiresAt: string): number {
  const diff = new Date(expiresAt).getTime() - Date.now()
  return Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
}

const loading = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const errorHintMessage = ref('')
const submitAttempted = ref(false)
const activeTab = ref<'recharge' | 'subscription'>('recharge')
const amount = ref<number | null>(null)
const selectedMethod = ref('')
const selectedNinePlusProductId = ref('')
const selectedNinePlusSubscriptionProductId = ref('')
const selectedPlan = ref<SubscriptionPlan | null>(null)
const previewImage = ref('')

const paymentPhase = ref<'select' | 'paying'>('select')

interface CreateOrderOptions {
  openid?: string
  wechatResumeToken?: string
  paymentType?: string
  externalProduct?: NinePlusProduct | null
  isResume?: boolean
  mobileQrFallbackAttempted?: boolean
}

interface WeixinJSBridgeLike {
  invoke(
    action: string,
    payload: Record<string, unknown>,
    callback: (result: Record<string, unknown>) => void,
  ): void
}

function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    createdAt: 0,
  }
}

function getWeixinJSBridge(): WeixinJSBridgeLike | undefined {
  return (window as Window & { WeixinJSBridge?: WeixinJSBridgeLike }).WeixinJSBridge
}

function waitForWeixinJSBridge(timeoutMs = 4000): Promise<WeixinJSBridgeLike | null> {
  const existing = getWeixinJSBridge()
  if (existing) return Promise.resolve(existing)

  return new Promise((resolve) => {
    let settled = false
    const finish = (bridge: WeixinJSBridgeLike | null) => {
      if (settled) return
      settled = true
      document.removeEventListener('WeixinJSBridgeReady', handleReady)
      document.removeEventListener('onWeixinJSBridgeReady', handleReady)
      window.clearTimeout(timer)
      resolve(bridge)
    }
    const handleReady = () => finish(getWeixinJSBridge() ?? null)
    const timer = window.setTimeout(() => finish(getWeixinJSBridge() ?? null), timeoutMs)
    document.addEventListener('WeixinJSBridgeReady', handleReady, false)
    document.addEventListener('onWeixinJSBridgeReady', handleReady, false)
  })
}

async function invokeWechatJsapiPayment(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  const bridge = await waitForWeixinJSBridge()
  if (!bridge) {
    throw new Error('WECHAT_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve) => {
    bridge.invoke('getBrandWCPayRequest', payload, (result) => resolve(result || {}))
  })
}

const paymentState = ref<PaymentRecoverySnapshot>(emptyPaymentState())

function persistRecoverySnapshot(snapshot: PaymentRecoverySnapshot) {
  if (typeof window === 'undefined' || !snapshot.orderId) return
  writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
}

function removeRecoverySnapshot() {
  if (typeof window === 'undefined') return
  clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
}

function resetPayment() {
  paymentPhase.value = 'select'
  paymentState.value = emptyPaymentState()
  removeRecoverySnapshot()
}

async function redirectToPaymentResult(state: PaymentRecoverySnapshot): Promise<void> {
  const query: Record<string, string | undefined> = {}
  if (state.orderId > 0) {
    query.order_id = String(state.orderId)
  }
  if (state.outTradeNo) {
    query.out_trade_no = state.outTradeNo
  }
  if (state.resumeToken) {
    query.resume_token = state.resumeToken
  }
  await router.push({
    path: '/payment/result',
    query,
  })
}

function buildWechatOAuthAuthorizeUrl(
  authorizeUrl: string,
  context: { paymentType: string; orderType: OrderType; planId?: number; orderAmount: number },
): string {
  const normalizedUrl = authorizeUrl.trim()
  if (!normalizedUrl || typeof window === 'undefined') {
    return normalizedUrl
  }

  try {
    const targetUrl = new URL(normalizedUrl, window.location.origin)
    const redirectPath = targetUrl.searchParams.get('redirect') || '/purchase'
    const redirectUrl = new URL(redirectPath, window.location.origin)
    const paymentType = normalizeVisibleMethod(context.paymentType) || context.paymentType.trim() || 'wxpay'

    redirectUrl.searchParams.set('payment_type', paymentType)
    redirectUrl.searchParams.set('order_type', context.orderType)

    if (context.planId) {
      redirectUrl.searchParams.set('plan_id', String(context.planId))
    } else {
      redirectUrl.searchParams.delete('plan_id')
    }

    if (context.orderAmount > 0) {
      redirectUrl.searchParams.set('amount', String(context.orderAmount))
    } else {
      redirectUrl.searchParams.delete('amount')
    }

    targetUrl.searchParams.set('redirect', `${redirectUrl.pathname}${redirectUrl.search}`)
    return targetUrl.toString()
  } catch {
    return normalizedUrl
  }
}

function onPaymentDone() {
  const wasSubscription = paymentState.value.orderType === 'subscription'
  resetPayment()
  selectedPlan.value = null
  if (wasSubscription) {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
}

function onPaymentSuccess() {
  removeRecoverySnapshot()
  authStore.refreshUser()
  if (paymentState.value.orderType === 'subscription') {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
}

function onPaymentSettled() {
  removeRecoverySnapshot()
}

// All checkout data from single API call
const checkout = ref<CheckoutInfoResponse>({
  methods: {}, global_min: 0, global_max: 0,
  plans: [], nineplus_products: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, help_text: '', help_image_url: '', stripe_publishable_key: '',
})

const quickRechargeAmounts = [10, 20, 50, 100, 200, 500, 1000]
const DEFAULT_MIN_RECHARGE_AMOUNT = 10
const DEFAULT_MAX_RECHARGE_AMOUNT = 500000

const tabs = computed(() => {
  const result: { key: 'recharge' | 'subscription'; label: string }[] = []
  if (!checkout.value.balance_disabled) result.push({ key: 'recharge', label: t('payment.tabTopUp') })
  result.push({ key: 'subscription', label: t('payment.tabSubscribe') })
  return result
})

const visibleMethods = computed(() => getVisibleMethods(checkout.value.methods))
const enabledMethods = computed(() => Object.keys(visibleMethods.value))
const isNinePlusSelected = computed(() => selectedMethod.value === 'nineplus')
function ninePlusProductCategory(product: NinePlusProduct): string {
  return (product.category || product.badge || '').trim()
}
function isNinePlusProductInStock(product: NinePlusProduct): boolean {
  return product.stock_count == null || product.stock_count > 0
}
function isNinePlusSubscriptionProduct(product: NinePlusProduct): boolean {
  const text = `${ninePlusProductCategory(product)} ${product.display_name} ${product.description}`.toLowerCase()
  return text.includes('套餐')
    || text.includes('月包')
    || text.includes('月卡')
    || text.includes('年包')
    || text.includes('年卡')
    || text.includes('会员')
    || text.includes('畅用')
    || text.includes('订阅')
    || text.includes('subscription')
    || text.includes('membership')
}
function ninePlusSubscriptionProductTitle(product: NinePlusProduct): string {
  return product.display_name
    .replace(/[：:]\s*\d+(?:\.\d+)?\s*元\/月[，,]?\s*(?:月包|月卡)?\s*包含\s*\d+\s*额度.*$/, '')
    .trim() || product.display_name
}
const activeNinePlusProducts = computed(() =>
  (checkout.value.nineplus_products || [])
    .filter(product => product.enabled && isNinePlusProductInStock(product))
    .sort((a, b) => a.sort_order - b.sort_order)
)
const availableNinePlusProducts = computed(() =>
  activeNinePlusProducts.value
    .filter(product => !isNinePlusSubscriptionProduct(product))
    .sort((a, b) => ninePlusProductPriceAmount(a) - ninePlusProductPriceAmount(b) || a.sort_order - b.sort_order)
)
const ninePlusSubscriptionProducts = computed(() =>
  activeNinePlusProducts.value
    .filter(isNinePlusSubscriptionProduct)
    .sort((a, b) => ninePlusProductPriceAmount(a) - ninePlusProductPriceAmount(b) || a.sort_order - b.sort_order)
)
const rechargeMethodTypes = computed(() =>
  enabledMethods.value.includes('nineplus') ? ['nineplus'] : enabledMethods.value
)
const defaultNinePlusProduct = computed(() => availableNinePlusProducts.value[0] ?? null)
const selectedNinePlusProduct = computed(() =>
  availableNinePlusProducts.value.find(product => product.product_id === selectedNinePlusProductId.value) ?? null
)
const effectiveNinePlusProduct = computed(() => selectedNinePlusProduct.value ?? defaultNinePlusProduct.value)
const defaultNinePlusSubscriptionProduct = computed(() => ninePlusSubscriptionProducts.value[0] ?? null)
const selectedNinePlusSubscriptionProduct = computed(() =>
  ninePlusSubscriptionProducts.value.find(product => product.product_id === selectedNinePlusSubscriptionProductId.value) ?? null
)
const effectiveNinePlusSubscriptionProduct = computed(() => selectedNinePlusSubscriptionProduct.value ?? defaultNinePlusSubscriptionProduct.value)
function roundCurrencyAmount(value: number): number {
  return Math.round(value * 100) / 100
}
function ninePlusProductPriceAmount(product: { price: number }): number {
  return product.price
}
function ninePlusProductFeeAmount(product: { price: number; fee?: number; payment_amount?: number }): number {
  if ((product.fee ?? 0) > 0) return roundCurrencyAmount(product.fee ?? 0)
  const paymentAmount = product.payment_amount ?? 0
  if (paymentAmount > product.price) return roundCurrencyAmount(paymentAmount - product.price)
  return 0
}
function ninePlusProductAmount(product: { price: number; fee?: number; payment_amount?: number }): number {
  if ((product.payment_amount ?? 0) > 0) return roundCurrencyAmount(product.payment_amount ?? 0)
  return roundCurrencyAmount(product.price + ninePlusProductFeeAmount(product))
}
function formatNinePlusCreditedAmount(product: { quota?: number; quota_unit?: string }): string {
  const quota = product.quota ?? 0
  if (quota <= 0) return ''
  const unit = (product.quota_unit || '').trim()
  const normalizedUnit = unit.toUpperCase()
  if (normalizedUnit === 'USD' || unit === '$') return `$${quota.toFixed(2)}`
  if (normalizedUnit === 'CNY' || unit === '¥') return `¥${quota.toFixed(2)}`
  return unit ? `${quota} ${unit}` : quota.toFixed(2)
}
const selectedNinePlusCreditedAmountLabel = computed(() =>
  effectiveNinePlusProduct.value ? formatNinePlusCreditedAmount(effectiveNinePlusProduct.value) : ''
)
const selectedNinePlusSubscriptionQuotaLabel = computed(() =>
  effectiveNinePlusSubscriptionProduct.value ? formatNinePlusCreditedAmount(effectiveNinePlusSubscriptionProduct.value) : ''
)
const selectedNinePlusProductPriceAmount = computed(() =>
  effectiveNinePlusProduct.value ? ninePlusProductPriceAmount(effectiveNinePlusProduct.value) : 0
)
const selectedNinePlusProductFeeAmount = computed(() =>
  effectiveNinePlusProduct.value ? ninePlusProductFeeAmount(effectiveNinePlusProduct.value) : 0
)
const selectedNinePlusSubscriptionProductPriceAmount = computed(() =>
  effectiveNinePlusSubscriptionProduct.value ? ninePlusProductPriceAmount(effectiveNinePlusSubscriptionProduct.value) : 0
)
const selectedNinePlusSubscriptionProductFeeAmount = computed(() =>
  effectiveNinePlusSubscriptionProduct.value ? ninePlusProductFeeAmount(effectiveNinePlusSubscriptionProduct.value) : 0
)
const ninePlusSubscriptionAmount = computed(() =>
  effectiveNinePlusSubscriptionProduct.value ? ninePlusProductAmount(effectiveNinePlusSubscriptionProduct.value) : 0
)
const validAmount = computed(() =>
  isNinePlusSelected.value && effectiveNinePlusProduct.value
    ? ninePlusProductAmount(effectiveNinePlusProduct.value)
    : amount.value ?? 0
)
const balanceRechargeMultiplier = computed(() => {
  const multiplier = checkout.value.balance_recharge_multiplier
  return Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 1
})
// 订阅 CNY 换算汇率（1 USD = X CNY）。0 = 未配置，订阅保持 price 直付（与后端 opt-in 条件严格镜像）。
const subscriptionUsdToCnyRate = computed(() => {
  const rate = checkout.value.subscription_usd_to_cny_rate
  return Number.isFinite(rate) && rate > 0 ? rate : 0
})
const creditedAmount = computed(() => Math.round((validAmount.value * balanceRechargeMultiplier.value) * 100) / 100)

// Adaptive grid: center single card, 2-col for 2 plans, 3-col for 3+
const planGridClass = computed(() => {
  const n = checkout.value.plans.length
  if (n <= 2) return 'grid grid-cols-1 gap-5 sm:grid-cols-2'
  return 'grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3'
})

// Check if an amount fits a method's [min, max]. 0 = no limit.
function amountFitsMethod(amt: number, methodType: string): boolean {
  if (amt <= 0) return true
  const ml = visibleMethods.value[methodType]
  if (!ml) return false
  if (ml.single_min > 0 && amt < ml.single_min) return false
  if (ml.single_max > 0 && amt > ml.single_max) return false
  return true
}

// Visible methods decide the amount range shown to users.
const globalMinAmount = computed(() => {
  const limits = Object.values(visibleMethods.value)
  if (limits.length === 0) return 0
  if (limits.some(limit => limit.single_min <= 0)) return 0
  return Math.min(...limits.map(limit => limit.single_min))
})
const globalMaxAmount = computed(() => {
  const limits = Object.values(visibleMethods.value)
  if (limits.length === 0) return 0
  if (limits.some(limit => limit.single_max <= 0)) return 0
  return Math.max(...limits.map(limit => limit.single_max))
})

// Selected method's limits (for validation and error messages)
const selectedLimit = computed(() => visibleMethods.value[selectedMethod.value])
const selectedCurrency = computed(() => normalizePaymentCurrency(selectedLimit.value?.currency))
const localeCode = computed(() => {
  const raw = i18n.locale as unknown
  if (typeof raw === 'string') return raw
  if (raw && typeof raw === 'object' && 'value' in raw) {
    return String((raw as { value?: string }).value || '')
  }
  return undefined
})

const accountDisplayName = computed(() =>
  user.value?.username || user.value?.email || t('payment.rechargeUi.defaultAccountName')
)
const currentBalanceAmount = computed(() => {
  const value = user.value?.balance
  return Number.isFinite(value) ? Number(value) : 0
})
const formattedCurrentBalance = computed(() => formatSelectedPaymentAmount(currentBalanceAmount.value))
const currentSubscription = computed(() =>
  activeSubscriptions.value.find(subscription => subscription.status === 'active') ?? null
)
const currentSubscriptionSummary = computed(() => {
  const subscription = currentSubscription.value
  if (!subscription) return null

  return {
    planName: subscription.group?.name || t('payment.groupFallback', { id: subscription.group_id }),
    platform: platformLabel(subscription.group?.platform || ''),
    remainingText: subscription.expires_at
      ? t('userSubscriptions.daysRemaining', { days: getDaysRemaining(subscription.expires_at) })
      : t('userSubscriptions.noExpiration'),
    pendingCount: subscription.pending_renewal_count || 0,
    pendingDays: (subscription.pending_renewals || []).reduce(
      (total, renewal) => total + renewal.validity_days,
      0
    ),
  }
})
const effectiveMinAmount = computed(() => globalMinAmount.value > 0 ? globalMinAmount.value : DEFAULT_MIN_RECHARGE_AMOUNT)
const effectiveMaxAmount = computed(() => globalMaxAmount.value > 0 ? globalMaxAmount.value : DEFAULT_MAX_RECHARGE_AMOUNT)

function currencyFractionDigits(currency: string): number {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
    }).resolvedOptions().maximumFractionDigits ?? 2
  } catch {
    return 2
  }
}

function roundPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** currencyFractionDigits(currency)
  return Math.round(value * factor) / factor
}

function ceilPaymentAmount(value: number, currency: string): number {
  if (!Number.isFinite(value)) return 0
  const factor = 10 ** currencyFractionDigits(currency)
  return Math.ceil(value * factor) / factor
}

function subscriptionPaymentAmountForCurrency(value: number, currency: string): number {
  const rate = subscriptionUsdToCnyRate.value
  if (rate <= 0 || currency !== DEFAULT_PAYMENT_CURRENCY) return roundPaymentAmount(value, currency)
  return roundPaymentAmount(value * rate, currency)
}

function formatSelectedPaymentAmount(value: number): string {
  return formatPaymentAmount(value, selectedCurrency.value, localeCode.value)
}

function formatCreditedAmount(value: number): string {
  return formatPaymentAmount(value, 'USD', localeCode.value)
}

function formatSelectedSubscriptionPaymentAmount(value: number): string {
  return formatSelectedPaymentAmount(subscriptionPaymentAmountForCurrency(value, selectedCurrency.value))
}

const methodOptions = computed<PaymentMethodOption[]>(() =>
  rechargeMethodTypes.value.map((type) => {
    const ml = visibleMethods.value[type]
    return {
      type,
      display_name: ml?.display_name,
      fee_rate: ml?.fee_rate ?? 0,
      available: ml?.available !== false && amountFitsMethod(validAmount.value, type),
    }
  })
)

const feeRate = computed(() => isNinePlusSelected.value ? 0 : checkout.value?.recharge_fee_rate ?? 0)
const feeAmount = computed(() =>
  feeRate.value > 0 && validAmount.value > 0
    ? Math.ceil(((validAmount.value * feeRate.value) / 100) * 100) / 100
    : 0
)
const totalAmount = computed(() =>
  feeRate.value > 0 && validAmount.value > 0
    ? Math.round((validAmount.value + feeAmount.value) * 100) / 100
    : validAmount.value
)
const formattedEstimatedCreditedAmount = computed(() => formatCreditedAmount(creditedAmount.value))

const amountError = computed(() => {
  if (validAmount.value <= 0) return ''
  if (!isNinePlusSelected.value && !Number.isInteger(validAmount.value)) {
    return t('payment.amountMustBeInteger')
  }
  if (!isNinePlusSelected.value && validAmount.value < effectiveMinAmount.value) {
    return t('payment.amountTooLow', { min: formatSelectedPaymentAmount(effectiveMinAmount.value) })
  }
  if (!isNinePlusSelected.value && validAmount.value > effectiveMaxAmount.value) {
    return t('payment.amountTooHigh', { max: formatSelectedPaymentAmount(effectiveMaxAmount.value) })
  }
  // No method can handle this amount
  if (!rechargeMethodTypes.value.some((m) => amountFitsMethod(validAmount.value, m))) {
    return t('payment.amountNoMethod')
  }
  // Selected method can't handle this amount (but others can)
  const ml = selectedLimit.value
  if (ml) {
    if (ml.single_min > 0 && validAmount.value < ml.single_min) return t('payment.amountTooLow', { min: formatSelectedPaymentAmount(ml.single_min) })
    if (ml.single_max > 0 && validAmount.value > ml.single_max) return t('payment.amountTooHigh', { max: formatSelectedPaymentAmount(ml.single_max) })
  }
  return ''
})

const canSubmit = computed(() =>
  validAmount.value > 0
    && (isNinePlusSelected.value || (validAmount.value >= effectiveMinAmount.value && validAmount.value <= effectiveMaxAmount.value))
    && !amountError.value
    && (!isNinePlusSelected.value || effectiveNinePlusProduct.value !== null)
    && amountFitsMethod(validAmount.value, selectedMethod.value)
    && selectedLimit.value?.available !== false
)

const subscriptionNinePlusMethodOptions = computed<PaymentMethodOption[]>(() => {
	if (!enabledMethods.value.includes('nineplus')) return []
	const ml = visibleMethods.value.nineplus
  return [{
    type: 'nineplus',
    fee_rate: ml?.fee_rate ?? 0,
    available: ml?.available !== false && amountFitsMethod(ninePlusSubscriptionAmount.value, 'nineplus'),
  }]
})

const canSubmitNinePlusSubscription = computed(() =>
  effectiveNinePlusSubscriptionProduct.value !== null
    && selectedMethod.value === 'nineplus'
    && ninePlusSubscriptionAmount.value > 0
    && amountFitsMethod(ninePlusSubscriptionAmount.value, 'nineplus')
    && visibleMethods.value.nineplus?.available !== false
)

const subPaymentAmount = computed(() => {
  const price = selectedPlan.value?.price ?? 0
  return subscriptionPaymentAmountForCurrency(price, selectedCurrency.value)
})

const subFeeAmount = computed(() => {
  if (feeRate.value <= 0 || subPaymentAmount.value <= 0) return 0
  return ceilPaymentAmount((subPaymentAmount.value * feeRate.value) / 100, selectedCurrency.value)
})

const subTotalAmount = computed(() => {
  if (feeRate.value <= 0 || subPaymentAmount.value <= 0) return subPaymentAmount.value
  return roundPaymentAmount(subPaymentAmount.value + subFeeAmount.value, selectedCurrency.value)
})

function subscriptionTotalAmountForCurrency(value: number, currency: string): number {
  const paymentAmount = subscriptionPaymentAmountForCurrency(value, currency)
  if (feeRate.value <= 0 || paymentAmount <= 0) return paymentAmount
  const fee = ceilPaymentAmount((paymentAmount * feeRate.value) / 100, currency)
  return roundPaymentAmount(paymentAmount + fee, currency)
}

// Subscription-specific: method options based on gateway pay amount
const subMethodOptions = computed<PaymentMethodOption[]>(() => {
  const price = selectedPlan.value?.price ?? 0
  return enabledMethods.value.filter(type => type !== 'nineplus').map((type) => {
    const ml = visibleMethods.value[type]
    const currency = normalizePaymentCurrency(ml?.currency)
    return {
      type,
      display_name: ml?.display_name,
      fee_rate: ml?.fee_rate ?? 0,
      available: ml?.available !== false && amountFitsMethod(subscriptionTotalAmountForCurrency(price, currency), type),
    }
  })
})

const canSubmitSubscription = computed(() =>
  selectedPlan.value !== null
    && amountFitsMethod(subTotalAmount.value, selectedMethod.value)
    && selectedLimit.value?.available !== false
)

// Auto-switch to first available method when current selection can't handle the amount
watch(() => [validAmount.value, selectedMethod.value] as const, ([amt, method]) => {
  if (amt <= 0 || amountFitsMethod(amt, method)) return
  const available = rechargeMethodTypes.value.find((m) => amountFitsMethod(amt, m))
  if (available) selectedMethod.value = available
})

watch(availableNinePlusProducts, (products) => {
  if (!products.length) {
    selectedNinePlusProductId.value = ''
    return
  }
  if (!products.some(product => product.product_id === selectedNinePlusProductId.value)) {
    selectedNinePlusProductId.value = products[0].product_id
  }
}, { immediate: true })

watch(ninePlusSubscriptionProducts, (products) => {
  if (!products.length) {
    selectedNinePlusSubscriptionProductId.value = ''
    return
  }
  if (!products.some(product => product.product_id === selectedNinePlusSubscriptionProductId.value)) {
    selectedNinePlusSubscriptionProductId.value = products[0].product_id
  }
}, { immediate: true })

watch(isNinePlusSelected, (selected) => {
  if (!selected || selectedNinePlusProductId.value) return
  selectedNinePlusProductId.value = availableNinePlusProducts.value[0]?.product_id || ''
})

watch(() => [activeTab.value, ninePlusSubscriptionProducts.value.length] as const, ([tab, productCount]) => {
  if (tab === 'subscription' && productCount > 0 && enabledMethods.value.includes('nineplus')) {
    selectedMethod.value = 'nineplus'
  }
})

// Renewal modal state
const showRenewalModal = ref(false)
const renewGroupId = ref<number | null>(null)
const renewalPlans = computed(() => {
  if (renewGroupId.value == null) return []
  return checkout.value.plans.filter(p => p.group_id === renewGroupId.value)
})

const selectedPlanDisplay = computed<SubscriptionPlanDisplay>(() => {
  if (!selectedPlan.value) {
    return { description: '', quotaSummary: '', validitySummary: '' }
  }
  return buildSubscriptionPlanDisplay(selectedPlan.value, buildSubscriptionPlanDisplayLabels(t))
})

function selectPlan(plan: SubscriptionPlan) {
  selectedPlan.value = plan
  if (selectedMethod.value === 'nineplus') {
    selectedMethod.value = enabledMethods.value.find(method => method !== 'nineplus') || ''
  }
  errorMessage.value = ''
}

function selectPlanFromModal(plan: SubscriptionPlan) {
  showRenewalModal.value = false
  renewGroupId.value = null
  selectedPlan.value = plan
  errorMessage.value = ''
}

function closeRenewalModal() {
  showRenewalModal.value = false
  renewGroupId.value = null
}

async function handleSubmitRecharge() {
  if (!canSubmit.value || submitting.value) return
  await createOrder(validAmount.value, 'balance')
}

async function confirmSubscribe() {
  if (!selectedPlan.value || submitting.value) return
  await createOrder(selectedPlan.value.price, 'subscription', selectedPlan.value.id)
}

async function handleSubmitNinePlusSubscription() {
  const product = effectiveNinePlusSubscriptionProduct.value
  if (!product || !canSubmitNinePlusSubscription.value || submitting.value) return
  await createOrder(ninePlusProductAmount(product), 'subscription', undefined, { externalProduct: product })
}

async function createOrder(orderAmount: number, orderType: OrderType, planId?: number, options: CreateOrderOptions = {}) {
  submitting.value = true
  submitAttempted.value = true
  errorMessage.value = ''
  errorHintMessage.value = ''
  const requestType = normalizeVisibleMethod(options.paymentType || selectedMethod.value) || options.paymentType || selectedMethod.value
  const defaultExternalProduct = orderType === 'subscription'
    ? effectiveNinePlusSubscriptionProduct.value
    : effectiveNinePlusProduct.value
  const ninePlusProduct = requestType === 'nineplus' ? options.externalProduct ?? defaultExternalProduct : null
  try {
    const payload = buildCreateOrderPayload({
      amount: orderAmount,
      paymentType: requestType,
      orderType,
      planId,
      externalProductId: ninePlusProduct?.product_id,
      externalQuantity: ninePlusProduct ? 1 : undefined,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && normalizeVisibleMethod(requestType) === 'alipay'),
    })
    if (options.openid) {
      payload.openid = options.openid
    }
    if (options.wechatResumeToken) {
      payload.wechat_resume_token = options.wechatResumeToken
    }

    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const openWindow = (url: string) => {
      const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) {
        window.location.href = url
      }
    }
    const visibleMethod = normalizeVisibleMethod(requestType) || requestType
    // When user clicks the dedicated Stripe button, leave method blank so the
    // landing page renders Stripe's full Payment Element (card/link/alipay/wxpay).
    const stripeMethod = visibleMethod === 'stripe'
      ? ''
      : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret && visibleMethod !== 'airwallex'
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const airwallexRouteUrl = result.client_secret && result.intent_id
      ? router.resolve({
        path: '/payment/airwallex',
        query: {
          order_id: String(result.order_id),
          out_trade_no: result.out_trade_no || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType,
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && visibleMethod === 'alipay'),
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
      airwallexRouteUrl,
    })

    if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
      window.location.href = buildWechatOAuthAuthorizeUrl(decision.oauth.authorize_url, {
        paymentType: visibleMethod,
        orderType,
        planId,
        orderAmount,
      })
      return
    }

    if (decision.kind === 'unhandled') {
      applyScenarioError({ reason: 'UNHANDLED_PAYMENT_SCENARIO' }, visibleMethod)
      return
    }

    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)

    if (decision.kind === 'stripe_popup') {
      openWindow(decision.paymentState.payUrl)
      return
    }
    if (decision.kind === 'stripe_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'airwallex_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'wechat_jsapi' && decision.jsapi) {
      try {
        const jsapiResult = await invokeWechatJsapiPayment(decision.jsapi as Record<string, unknown>)
        const errMsg = String(jsapiResult.err_msg || '').toLowerCase()
        if (errMsg.includes('cancel')) {
          appStore.showInfo(t('payment.qr.cancelled'))
          resetPayment()
        } else if (errMsg && !errMsg.includes('ok')) {
          resetPayment()
          const fallbackApplied = await attemptMobileQrFallback(
            { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
            {
              orderAmount,
              orderType,
              planId,
              paymentType: visibleMethod,
              externalProduct: ninePlusProduct,
              attempted: options.mobileQrFallbackAttempted === true,
            },
          )
          if (!fallbackApplied) {
            applyScenarioError({ reason: 'WECHAT_JSAPI_FAILED', message: errMsg }, visibleMethod)
          }
        } else {
          const resultState = { ...decision.paymentState }
          resetPayment()
          await redirectToPaymentResult(resultState)
        }
      } catch (err: unknown) {
        resetPayment()
        const fallbackApplied = await attemptMobileQrFallback(err, {
          orderAmount,
          orderType,
          planId,
          paymentType: visibleMethod,
          externalProduct: ninePlusProduct,
          attempted: options.mobileQrFallbackAttempted === true,
        })
        if (!fallbackApplied) {
          throw err
        }
      }
      return
    }
    if (decision.kind === 'redirect_waiting' && decision.paymentState.payUrl) {
      if (isMobileDevice()) {
        window.location.href = decision.paymentState.payUrl
        return
      }
      openWindow(decision.paymentState.payUrl)
    }
  } catch (err: unknown) {
    const apiErr = err as Record<string, unknown>
    if (apiErr.reason === 'TOO_MANY_PENDING') {
      const metadata = apiErr.metadata as Record<string, unknown> | undefined
      errorMessage.value = t('payment.errors.tooManyPending', { max: metadata?.max || '' })
      errorHintMessage.value = ''
    } else if (apiErr.reason === 'CANCEL_RATE_LIMITED') {
      errorMessage.value = t('payment.errors.cancelRateLimited')
      errorHintMessage.value = ''
    } else if (await attemptMobileQrFallback(err, {
      orderAmount,
      orderType,
      planId,
      paymentType: requestType,
      externalProduct: ninePlusProduct,
      attempted: options.mobileQrFallbackAttempted === true,
    })) {
      return
    } else {
      const handled = applyScenarioError(
        err,
        normalizeVisibleMethod(options.paymentType || selectedMethod.value) || selectedMethod.value,
      )
      if (!handled) {
        errorMessage.value = extractI18nErrorMessage(err, t, 'payment.errors', extractApiErrorMessage(err, t('payment.result.failed')))
        errorHintMessage.value = ''
      }
      if (handled) {
        return
      }
    }
    appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  } finally {
    submitting.value = false
  }
}

interface MobileQrFallbackContext {
  orderAmount: number
  orderType: OrderType
  planId?: number
  paymentType: string
  externalProduct?: NinePlusProduct | null
  attempted: boolean
}

function shouldFallbackToDesktopQr(err: unknown, paymentMethod: string, attempted: boolean): boolean {
  if (attempted || !isMobileDevice()) {
    return false
  }

  const normalizedMethod = normalizeVisibleMethod(paymentMethod) || paymentMethod
  const reason = typeof err === 'object' && err && 'reason' in err && typeof err.reason === 'string'
    ? err.reason
    : ''
  const message = err instanceof Error
    ? err.message
    : (typeof err === 'object' && err && 'message' in err && typeof err.message === 'string'
      ? err.message
      : '')
  const normalizedMessage = message.toLowerCase()

  if (normalizedMethod === 'wxpay') {
    return reason === 'WECHAT_H5_NOT_AUTHORIZED'
      || reason === 'WECHAT_PAYMENT_MP_NOT_CONFIGURED'
      || reason === 'WECHAT_JSAPI_FAILED'
      || reason === 'PAYMENT_GATEWAY_ERROR'
      || reason === 'UNHANDLED_PAYMENT_SCENARIO'
      || normalizedMessage.includes('weixinjsbridge is unavailable')
      || normalizedMessage.includes('wechat_jsapi_unavailable')
  }

  if (normalizedMethod === 'alipay') {
    return reason === 'PAYMENT_GATEWAY_ERROR' || reason === 'UNHANDLED_PAYMENT_SCENARIO'
  }

  return false
}

async function attemptMobileQrFallback(err: unknown, context: MobileQrFallbackContext): Promise<boolean> {
  if (!shouldFallbackToDesktopQr(err, context.paymentType, context.attempted)) {
    return false
  }

  try {
    const visibleMethod = normalizeVisibleMethod(context.paymentType) || context.paymentType
    const ninePlusProduct = visibleMethod === 'nineplus' ? context.externalProduct ?? effectiveNinePlusProduct.value : null
    const payload = buildCreateOrderPayload({
      amount: context.orderAmount,
      paymentType: visibleMethod,
      orderType: context.orderType,
      planId: context.planId,
      externalProductId: ninePlusProduct?.product_id,
      externalQuantity: ninePlusProduct ? 1 : undefined,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: false,
      isWechatBrowser: false,
    })
    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const stripeMethod = visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      orderType: context.orderType,
      isMobile: false,
      isWechatBrowser: false,
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
    })

    if (decision.kind !== 'qr_waiting' || !decision.paymentState.qrCode) {
      return false
    }

    errorMessage.value = ''
    errorHintMessage.value = ''
    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)
    appStore.showWarning(t('payment.errors.mobilePaymentFallbackToQr'))
    return true
  } catch {
    return false
  }
}

function applyScenarioError(err: unknown, paymentMethod: string): boolean {
  const descriptor = describePaymentScenarioError(err, {
    paymentMethod,
    isMobile: isMobileDevice(),
    isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
  })
  if (!descriptor) {
    errorMessage.value = ''
    errorHintMessage.value = ''
    return false
  }
  errorMessage.value = t(descriptor.messageKey)
  errorHintMessage.value = descriptor.hintKey ? t(descriptor.hintKey) : ''
  appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  return true
}

async function resumeWechatPaymentFromQuery() {
  const resume = parseWechatResumeRoute(route.query, checkout.value.plans, validAmount.value)
  if (!resume) {
    return
  }

  selectedMethod.value = resume.paymentType
  if (resume.orderType === 'balance' && resume.orderAmount > 0) {
    amount.value = resume.orderAmount
  }
  if (resume.orderType === 'subscription' && resume.planId) {
    selectedPlan.value = checkout.value.plans.find(plan => plan.id === resume.planId) ?? null
  }

  await router.replace({ path: route.path, query: stripWechatResumeQuery(route.query) })

  if (resume.wechatResumeToken) {
    await createOrder(0, resume.orderType, resume.planId, {
      wechatResumeToken: resume.wechatResumeToken,
      paymentType: resume.paymentType,
      isResume: true,
    })
    return
  }

  if (resume.orderAmount > 0 && resume.openid) {
    await createOrder(resume.orderAmount, resume.orderType, resume.planId, {
      openid: resume.openid,
      paymentType: resume.paymentType,
      isResume: true,
    })
  }
}

onMounted(async () => {
  try {
    const res = await paymentAPI.getCheckoutInfo()
    checkout.value = res.data
    selectedNinePlusProductId.value = availableNinePlusProducts.value[0]?.product_id || ''
    if (rechargeMethodTypes.value.length) {
      const order: readonly string[] = METHOD_ORDER
      const sorted = [...rechargeMethodTypes.value].sort((a, b) => {
        const ai = order.indexOf(a)
        const bi = order.indexOf(b)
        return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi)
      })
      selectedMethod.value = sorted[0]
    }
    if (typeof window !== 'undefined') {
      if (hasWechatResumeQuery(route.query)) {
        removeRecoverySnapshot()
      }
      const routeResumeToken = typeof route.query.resume_token === 'string'
        ? route.query.resume_token
        : typeof route.query.wechat_resume_token === 'string'
          ? route.query.wechat_resume_token
          : undefined
      const restored = readPaymentRecoverySnapshot(
        window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
        { resumeToken: routeResumeToken },
      )
      if (restored) {
        paymentState.value = restored
        paymentPhase.value = 'paying'
        const restoredMethod = normalizeVisibleMethod(restored.paymentType)
          || (visibleMethods.value[restored.paymentType] ? restored.paymentType : '')
        if (restoredMethod) {
          selectedMethod.value = restoredMethod
        }
      } else {
        removeRecoverySnapshot()
      }
    }
    await resumeWechatPaymentFromQuery()
    if (checkout.value.balance_disabled) {
      activeTab.value = 'subscription'
    }
    // Handle renewal navigation: ?tab=subscription&group=123
    if (route.query.tab === 'subscription') {
      activeTab.value = 'subscription'
      if (route.query.group) {
        const groupId = Number(route.query.group)
        const groupPlans = checkout.value.plans.filter(p => p.group_id === groupId)
        if (groupPlans.length === 1) {
          selectedPlan.value = groupPlans[0]
        } else if (groupPlans.length > 1) {
          renewGroupId.value = groupId
          showRenewalModal.value = true
        }
      }
    }
  } catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { loading.value = false }
  // Fetch active subscriptions (uses cache, non-blocking)
  subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
</script>

<style scoped>
.recharge-page-canvas {
  min-height: calc(100vh - 4rem);
  margin: -1rem;
  padding: 1rem;
  background:
    radial-gradient(circle at 18% 0%, rgba(59, 130, 246, 0.1), transparent 30%),
    radial-gradient(circle at 88% 8%, rgba(96, 165, 250, 0.14), transparent 26%),
    linear-gradient(180deg, #f6f9ff 0%, #eef4fb 100%);
}

.recharge-summary-card {
  position: sticky;
  top: 6rem;
}

@media (min-width: 768px) {
  .recharge-page-canvas {
    margin: -1.5rem;
    padding: 1.5rem;
  }
}

@media (min-width: 1024px) {
  .recharge-page-canvas {
    margin: -2rem;
    padding: 2rem;
  }
}

@media (max-width: 767px) {
  .recharge-summary-card {
    position: static;
  }
}
</style>
