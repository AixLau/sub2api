const PAYMENT_METHOD_COLOR_CLASS: Record<string, string> = {
  alipay: 'bg-provider-alipay',
  alipay_direct: 'bg-provider-alipay-deep',
  wxpay: 'bg-provider-wechat',
  wxpay_direct: 'bg-provider-wechat-deep',
  wechat: 'bg-provider-wechat',
  wechat_pay: 'bg-provider-wechat',
  stripe: 'bg-provider-stripe',
  airwallex: 'bg-provider-airwallex-selection',
}

export function paymentMethodColorClass(method: string): string {
  return PAYMENT_METHOD_COLOR_CLASS[method] || 'bg-content-disabled'
}
