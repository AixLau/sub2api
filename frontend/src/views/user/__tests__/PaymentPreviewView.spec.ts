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
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
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
    'payment.purchaseHero.lineOne': '让每一次创造',
    'payment.purchaseHero.lineTwoLead': '都有',
    'payment.purchaseHero.lineTwoAccent': '能量',
    'payment.purchaseHero.securityLine': 'SECURE · FAST · RELIABLE',
    'payment.planCard.monthlyLimit': '月限额',
    'payment.planCard.quota': '配额',
    'payment.planCard.quotaUnit': '额度',
    'payment.planCard.rate': '倍率',
    'payment.planCard.validitySuffix': '有效',
    'payment.days': '天',
    'payment.weeks': '周',
    'payment.months': '个月',
    'payment.years': '年',
    'payment.perMonth': '月',
    'payment.noPlans': '暂无可用订阅套餐',
    'payment.selectPlan': '选择套餐',
    'payment.subscribeNow': '立即开通',
    'payment.renewNow': '立即续费',
    'payment.tabSubscribe': '订阅服务',
    'payment.tabTopUp': '余额充值',
    'payment.activeSubscription': '当前订阅',
    'payment.rechargeUi.activeMode': '当前模式',
    'payment.rechargeUi.agreementName': '充值协议',
    'payment.rechargeUi.agreementPrefix': '我已阅读并同意',
    'payment.rechargeUi.amountHint': '选择快捷金额，或输入自定义充值金额。',
    'payment.rechargeUi.arrivalTime': '到账时间',
    'payment.rechargeUi.availableBalance': '当前可用余额',
    'payment.rechargeUi.cardMethodDesc': '安全快捷',
    'payment.rechargeUi.customAmountPlaceholder': '请输入金额',
    'payment.rechargeUi.creditedBalance': '到账 API 余额',
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
    'payment.rechargeUi.oneToOneAgreement': '¥1 = $1 API 余额',
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
    'payment.subscriptionUi.activationRule': '生效方式',
    'payment.subscriptionUi.deferredActivation': '当前套餐结束后顺延',
    'payment.subscriptionUi.immediateActivation': '支付成功后立即生效',
    'payment.subscriptionUi.manualRenewal': '到期后手动续费',
    'payment.subscriptionUi.noAutomaticRenewal': '不会自动扣款',
    'payment.subscriptionUi.oneTimePurchase': '一次性购买',
    'payment.subscriptionUi.originalPrice': '套餐原价',
    'payment.subscriptionUi.planDiscount': '套餐优惠',
    'payment.subscriptionUi.renewalMethod': '续费方式',
    'payment.subscriptionUi.renewalTarget': '当前套餐同组续费',
    'payment.subscriptionUi.selectPlanFirst': '请先选择订阅套餐',
    'payment.subscriptionUi.selectedPlan': '订阅套餐',
    'payment.subscriptionUi.validity': '服务有效期',
    'userSubscriptions.daysRemaining': '剩余 {days} 天',
    'userSubscriptions.pendingRenewals': '待生效续费 {count} 笔',
    'userSubscriptions.pendingRenewalTotalDays': '共 {days} 天',
    'userSubscriptions.pendingRenewalRule': '当前套餐结束后按付款顺序生效。',
  }
  return {
    ...actual,
    useI18n: () => ({
      locale: ref('zh'),
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
  it('keeps the compact top tabs ahead of the shared hero and previews exact credited balance', async () => {
    routeState.query = {}
    routerReplace.mockReset()
    const wrapper = mount(PaymentPreviewView)

    const previewShell = wrapper.get('[data-testid="purchase-preview-shell"]')
    expect(previewShell.classes()).toEqual(expect.arrayContaining(['purchase-select-shell', 'max-w-none']))

    const tabList = wrapper.get('[role="tablist"]').element
    const hero = wrapper.get('[data-testid="purchase-hero"]').element
    expect(tabList.compareDocumentPosition(hero) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0)

    expect(wrapper.find('[data-testid="recharge-liquid-page"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="purchase-hero"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="purchase-balance-ticket"]').text()).toContain('$7,365.87')
    expect(wrapper.get('[data-testid="purchase-mode-recharge"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="purchase-mode-subscription"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="recharge-preview-panel"]').attributes('role')).toBe('tabpanel')
    expect(wrapper.get('[data-testid="recharge-preview-panel"]').classes()).toContain('purchase-business-panel')
    expect(wrapper.find('[data-testid="recharge-layout"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="recharge-controls"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="submit-recharge"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="purchase-energy-orb"]').attributes('src'))
      .toBe('/assets/purchase/energy-orb.webp')
    expect(wrapper.text()).not.toContain('选择快捷金额，或输入自定义充值金额。')
    expect(wrapper.text()).not.toContain('使用安全支付通道完成本次充值。')

    const decorations = wrapper.get('[data-testid="purchase-decorations"]')
    expect(decorations.findAll('img')).toHaveLength(2)
    for (const image of decorations.findAll('img')) {
      expect(image.attributes('aria-hidden')).toBe('true')
      expect(image.attributes('draggable')).toBe('false')
      expect(image.attributes('alt')).toBe('')
    }

    expect(wrapper.find('[data-testid="purchase-right-rail"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="purchase-hero"]')
      .find('[data-testid="purchase-balance-ticket"]').exists()).toBe(false)

    const quickAmount10 = wrapper.get('[data-testid="quick-amount-10"]')
    const quickAmount100 = wrapper.get('[data-testid="quick-amount-100"]')
    expect(quickAmount10.text()).toContain('$10.00')
    expect(quickAmount100.text()).toContain('$100.00')

    await quickAmount10.trigger('click')
    expect(wrapper.get('[data-testid="order-summary"]').text()).toContain('¥10.00')
    expect(wrapper.get('[data-testid="estimated-credited-highlight"]').text()).toContain('$10.00')
    expect(wrapper.find('[data-testid="recharge-one-to-one-agreement"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('¥1 = $1 API 余额')

    await quickAmount100.trigger('click')
    expect(wrapper.findAll('[data-testid^="payment-method-"]')).toHaveLength(2)
    expect(wrapper.find('[data-testid="payment-method-stripe"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="payment-method-airwallex"]').exists()).toBe(false)
    await wrapper.get('[data-testid="payment-method-wxpay"]').trigger('click')

    expect(wrapper.get('[data-testid="estimated-credited-highlight"]').text()).toContain('$100.00')
    expect(wrapper.get('[data-testid="order-summary"]').text()).toContain('¥100.00')
    expect(wrapper.text()).not.toContain('$13.83')
    expect(wrapper.text()).not.toMatch(/优惠券|自动续费已开启|下次(?:自动)?扣款|下一续费时间/)
    expect(wrapper.find('[data-testid*="coupon"]').exists()).toBe(false)

    await wrapper.get('[data-testid="submit-recharge"]').trigger('click')
    expect(wrapper.get('[data-testid="purchase-preview-notice"]').attributes('role')).toBe('status')
  })

  it('renders three API-shaped plans, keeps them visible, and preserves recovery query on tab changes', async () => {
    routeState.query = {
      tab: 'subscription',
      group: '101',
      resume_token: 'resume-token',
      wechat_resume_token: 'wechat-token',
    }
    routerReplace.mockReset()
    const wrapper = mount(PaymentPreviewView)

    expect(wrapper.get('[data-testid="purchase-mode-subscription"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="subscription-preview-panel"]').attributes('role')).toBe('tabpanel')
    expect(wrapper.get('[data-testid="subscription-preview-panel"]').classes()).toContain('purchase-business-panel')
    expect(wrapper.find('[data-testid="subscription-layout"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="subscription-controls"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="submit-subscription"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="purchase-balance-ticket"]').text()).toContain('$7,365.87')
    expect(wrapper.findAll('[data-testid^="subscription-plan-"][role="radio"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="subscription-plan-101"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="current-subscription-name"]').text()).toBe('创作月度套餐')
    expect(wrapper.find('[data-testid="subscription-no-automatic-renewal"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="subscription-manual-renewal"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="order-summary"]').text()).not.toContain('一次性购买')
    expect(wrapper.text()).not.toContain('优惠券')
    expect(wrapper.text()).not.toContain('下次扣款')
    expect(wrapper.text()).not.toContain('自动续费已开启')
    expect(wrapper.text()).not.toContain('下一续费时间')
    expect(wrapper.text()).not.toMatch(/≈\s*\$/)
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid*="coupon"]').exists()).toBe(false)

    await wrapper.get('[data-testid="subscription-plan-102"]').trigger('click')

    expect(wrapper.findAll('[data-testid^="subscription-plan-"][role="radio"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="subscription-summary-plan"]').text()).toBe('创作季度套餐')

    await wrapper.get('[data-testid="purchase-mode-recharge"]').trigger('click')

    expect(routerReplace).toHaveBeenCalledWith({
      query: {
        tab: 'recharge',
        resume_token: 'resume-token',
        wechat_resume_token: 'wechat-token',
      },
    })
    expect(wrapper.get('[data-testid="purchase-mode-recharge"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="recharge-preview-panel"]').exists()).toBe(true)
  })
})
