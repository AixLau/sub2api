import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import type { SubscriptionPlan } from "@/types/payment";
import SubscriptionPlanCard from "../SubscriptionPlanCard.vue";

const i18n = createI18n({
  legacy: false,
  locale: "en",
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      payment: {
        days: "days",
        weeks: "weeks",
        months: "months",
        perMonth: "month",
        models: "Models",
        planCard: {
          quota: "Quota",
          quotaUnit: "额度",
          validitySuffix: "有效",
          rate: "Rate",
          dailyLimit: "Daily",
          weeklyLimit: "Weekly",
          monthlyLimit: "Monthly",
          unlimited: "Unlimited",
        },
        subscribeNow: "Subscribe now",
      },
    },
  },
});

const mountPlanCard = (groupPlatform: string, overrides: Partial<SubscriptionPlan> = {}) =>
  mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1,
        group_id: 10,
        group_platform: groupPlatform,
        name: "Pro",
        description: "包含 400 额度 / 30 天",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1.3,
        daily_limit_usd: 0,
        weekly_limit_usd: 0,
        monthly_limit_usd: 400,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: ["claude", "gemini_text", "gemini_image"],
        is_active: true,
        ...overrides,
      },
    },
    global: { plugins: [i18n, createPinia()] },
  });

describe("SubscriptionPlanCard", () => {
  it("uses the liquid glass subscription plan card surface", () => {
    const wrapper = mountPlanCard("openai");

    expect(wrapper.classes()).toContain("subscription-liquid-plan-card");
    expect(wrapper.find("[data-testid='subscription-plan-price']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='subscription-plan-features']").exists()).toBe(false);
  });

  it("uses the compact purchase-card layout without rate or limit detail blocks", () => {
    const wrapper = mountPlanCard("openai");
    const text = wrapper.text();

    expect(wrapper.find("[data-testid='subscription-plan-description']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='subscription-plan-quota-summary']").text()).toBe("400 额度");
    expect(wrapper.find("[data-testid='subscription-plan-validity-summary']").text()).toBe("30 天有效");
    expect(wrapper.find("[data-testid='subscription-plan-price']").text()).toBe("¥10.00");
    expect(text).not.toContain("包含 400 额度 / 30 天");
    expect(text).not.toContain("/ 30");
    expect(wrapper.find("[data-testid='subscription-plan-quota']").exists()).toBe(false);
    expect(text).not.toContain("OpenAI");
    expect(text).not.toContain("$400");
    expect(text).not.toContain("×1.3");
    expect(text).not.toContain("Rate");
    expect(text).not.toContain("Daily");
    expect(text).not.toContain("Weekly");
    expect(text).not.toContain("Monthly Limit");
    expect(wrapper.find(".subscription-detail-grid").exists()).toBe(false);
  });

  it("removes generated quota-only description copy from plan cards", () => {
    const wrapper = mountPlanCard("openai", {
      description: "包含 400 额度",
    });
    const text = wrapper.text();

    expect(wrapper.find("[data-testid='subscription-plan-description']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='subscription-plan-quota-summary']").text()).toBe("400 额度");
    expect(text).not.toContain("包含 400 额度");
  });

  it("removes generated quota description even when the backend quota field differs", () => {
    const wrapper = mountPlanCard("openai", {
      description: "包含 500 额度",
      monthly_limit_usd: 400,
    });
    const text = wrapper.text();

    expect(wrapper.find("[data-testid='subscription-plan-description']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='subscription-plan-quota-summary']").text()).toBe("400 额度");
    expect(text).not.toContain("包含 500 额度");
  });

  it("does not show platform model scopes on compact plan cards", () => {
    const text = mountPlanCard("openai").text();

    expect(text).not.toContain("Claude");
    expect(text).not.toContain("Gemini");
    expect(text).not.toContain("Imagen");

    const antigravityText = mountPlanCard("antigravity").text();
    expect(antigravityText).not.toContain("Claude");
    expect(antigravityText).not.toContain("Gemini");
    expect(antigravityText).not.toContain("Imagen");
  });

  // #4607：管理端保存的单位是复数（months/weeks），此前用户侧只匹配单数
  // 'month'，「1 个月」的套餐卡片被显示成「1天」。测试环境的 vue-i18n 为
  // runtime-only 构建，t() 原样返回 key，故按 key 断言单位分支。
  it("renders plural admin-form validity units instead of mislabeled days (#4607)", () => {
    expect(mountPlanCard("openai", { validity_days: 1, validity_unit: "months" }).text()).toContain("/ payment.perMonth");
    expect(mountPlanCard("openai", { validity_days: 3, validity_unit: "months" }).text()).toContain("/ 3payment.months");
    expect(mountPlanCard("openai", { validity_days: 2, validity_unit: "weeks" }).text()).toContain("/ 2payment.weeks");
    expect(mountPlanCard("openai", { validity_days: 30, validity_unit: "day" }).text()).toContain("30 天有效");
  });

  it("uses the configured currency symbol while preserving USD for legacy plans", () => {
    const cnyPlan = mountPlanCard("openai", { currency: "CNY", original_price: 20 });
    const usdPlan = mountPlanCard("openai", { currency: "USD" });

    expect(cnyPlan.find("[data-testid='subscription-plan-price']").text()).toBe("¥10.00");
    expect(cnyPlan.text()).toContain("¥20.00");
    expect(usdPlan.find("[data-testid='subscription-plan-price']").text()).toBe("$10.00");
  });
});
