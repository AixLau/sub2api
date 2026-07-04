import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PaymentPreviewView from '../PaymentPreviewView.vue'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))
const routerReplace = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({
      replace: routerReplace,
    }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const translations: Record<string, string> = {
    'common.processing': '处理中...',
    'payment.actualPay': '实付金额',
    'payment.amountTooHigh': '最高金额为 {max}',
    'payment.amountTooLow': '最低金额为 {min}',
    'payment.fee': '手续费',
    'payment.methods.airwallex': 'Airwallex',
    'payment.methods.alipay': '支付宝',
    'payment.methods.wxpay': '微信支付',
    'payment.packagePrice': '套餐价格',
    'payment.payableAmount': '应付金额',
    'payment.paymentAmount': '支付金额',
    'payment.paymentMethod': '支付方式',
    'payment.planCard.monthlyLimit': '月限额',
    'payment.planCard.quota': '配额',
    'payment.planCard.rate': '倍率',
    'payment.subscribeNow': '立即开通',
    'payment.tabSubscribe': '订阅',
    'payment.tabTopUp': '充值',
    'payment.activeSubscription': '当前订阅',
    'payment.rechargeUi.activeMode': '当前模式',
    'payment.rechargeUi.accountId': '账户 ID',
    'payment.rechargeUi.agreementName': '充值协议',
    'payment.rechargeUi.agreementPrefix': '我已阅读并同意',
    'payment.rechargeUi.amountHint': '选择快捷金额，或输入自定义充值金额。',
    'payment.rechargeUi.arrivalTime': '到账时间',
    'payment.rechargeUi.availableBalance': '当前可用余额',
    'payment.rechargeUi.cardMethodDesc': '安全快捷',
    'payment.rechargeUi.customAmountPlaceholder': '请输入金额',
    'payment.rechargeUi.enterpriseCard': '银行卡支付',
    'payment.rechargeUi.enterpriseEncryption': '企业级加密保护',
    'payment.rechargeUi.enterpriseEncryptionDesc': 'SSL 加密传输，数据安全存储。',
    'payment.rechargeUi.estimatedCreditedAmount': '预计到账金额',
    'payment.rechargeUi.fastAndSecure': '安全快捷',
    'payment.rechargeUi.globalMethodDesc': '全球支付网络',
    'payment.rechargeUi.instantArrival': '实时到账',
    'payment.rechargeUi.instantArrivalShort': '实时到账',
    'payment.rechargeUi.methodUnavailable': '当前金额不可用',
    'payment.rechargeUi.noFee': '优惠 0',
    'payment.rechargeUi.orderSummary': '订单摘要',
    'payment.rechargeUi.paymentMethodHint': '使用安全支付通道完成本次充值。',
    'payment.rechargeUi.previewNotice': '这是本地 UI 预览，不会发起真实支付请求。',
    'payment.rechargeUi.rechargeNow': '立即充值',
    'payment.rechargeUi.recommended': '推荐',
    'payment.rechargeUi.retryRecharge': '重新发起充值',
    'payment.rechargeUi.realTimeArrival': '资金实时到账',
    'payment.rechargeUi.realTimeArrivalDesc': '充值成功后，资金立即到账。',
    'payment.rechargeUi.securePayment': '支付安全保障',
    'payment.rechargeUi.securePaymentDesc': '多重风控体系，保障资金安全。',
    'payment.rechargeUi.selectAmount': '选择充值金额',
    'payment.rechargeUi.selectPaymentMethod': '选择支付方式',
    'payment.rechargeUi.selectedPlan': '已选套餐',
    'payment.rechargeUi.subscriptionHint': '选择企业订阅套餐，并通过同一套安全收银台完成支付。',
    'payment.rechargeUi.subtitle': '快速为账户充值，支持多种支付方式，资金实时到账。',
    'payment.rechargeUi.support247': '7x24 小时客服支持',
    'payment.rechargeUi.support247Desc': '专业团队，随时为您服务。',
    'payment.rechargeUi.title': '账户充值',
    'payment.rechargeUi.trustGuarantees': '充值安全保障',
    'payment.rechargeUi.verified': '企业认证',
    'userSubscriptions.daysRemaining': '剩余 {days} 天',
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const template = translations[key] ?? key
        if (!params) return template
        return Object.entries(params).reduce(
          (text, [name, value]) => text.replaceAll(`{${name}}`, String(value)),
          template,
        )
      },
    }),
  }
})

describe('PaymentPreviewView', () => {
  it('renders a login-free recharge preview with interactive amount and method selection', async () => {
    routeState.query = {}
    routerReplace.mockReset()
    const wrapper = mount(PaymentPreviewView)

    expect(wrapper.find('[data-testid="recharge-liquid-page"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="account-balance-hero"]').text()).toContain('Acme Corporation')
    expect(wrapper.find('[data-testid="purchase-mode-recharge"]').attributes('aria-pressed')).toBe('true')

    await wrapper.find('[data-testid="quick-amount-100"]').trigger('click')
    expect(wrapper.find('[data-testid="order-summary"]').text()).toContain('¥100.00')

    await wrapper.find('[data-testid="payment-method-wxpay"]').trigger('click')
    expect(wrapper.find('[data-testid="payment-method-wxpay"]').attributes('aria-checked')).toBe('true')
  })

  it('switches to the subscription preview and keeps it reachable from the tab query', async () => {
    routeState.query = {
      tab: 'subscription',
    }
    routerReplace.mockReset()
    const wrapper = mount(PaymentPreviewView)

    expect(wrapper.find('[data-testid="purchase-mode-subscription"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-testid="subscription-preview-layout"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('团队专业版')
    expect(wrapper.find('[data-testid="recharge-header-account-pill"]').exists()).toBe(false)
    const heroText = wrapper.find('[data-testid="account-balance-hero"]').text()
    expect(heroText).toContain('当前订阅')
    expect(heroText).toContain('Pro 套餐')
    expect(heroText).toContain('OpenAI')
    expect(heroText).toContain('剩余 21 天')
    expect(heroText).not.toContain('当前可用余额')
    expect(heroText).not.toContain('¥12,345.67')

    await wrapper.find('[data-testid="purchase-mode-recharge"]').trigger('click')

    expect(routerReplace).toHaveBeenCalledWith({ query: { tab: 'recharge' } })
    expect(wrapper.find('[data-testid="purchase-mode-recharge"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.find('[data-testid="recharge-preview-layout"]').exists()).toBe(true)
  })
})
