<template>
  <div
    v-if="tabs.length > 0"
    ref="tablistRef"
    class="purchase-mode-tabs"
    role="tablist"
    :aria-label="t('payment.rechargeUi.activeMode')"
    :style="{ gridTemplateColumns: `repeat(${tabs.length}, minmax(0, 1fr))` }"
  >
    <button
      v-for="(tab, index) in tabs"
      :key="tab.key"
      type="button"
      class="purchase-mode-tab"
      :class="{ 'purchase-mode-tab--active': modelValue === tab.key }"
      role="tab"
      :id="`purchase-tab-${tab.key}`"
      :aria-selected="modelValue === tab.key"
      :aria-pressed="modelValue === tab.key"
      :aria-controls="tab.panelId ?? `purchase-panel-${tab.key}`"
      :tabindex="modelValue === tab.key || (activeTabIndex < 0 && index === 0) ? 0 : -1"
      :data-testid="`purchase-mode-${tab.key}`"
      @click="selectTab(tab.key)"
      @keydown="handleKeydown($event, index)"
    >
      <Icon
        :name="tab.key === 'recharge' ? 'bolt' : 'calendar'"
        size="sm"
        :stroke-width="2.2"
        aria-hidden="true"
      />
      <span class="purchase-mode-tab__label">{{ tab.label }}</span>
      <span v-if="tab.recommended" class="purchase-mode-tab__badge">
        {{ t('payment.rechargeUi.recommended') }}
      </span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

type PurchaseMode = 'recharge' | 'subscription'

const props = defineProps<{
  modelValue: PurchaseMode
  tabs: Array<{
    key: PurchaseMode
    label: string
    recommended?: boolean
    panelId?: string
  }>
}>()

const emit = defineEmits<{
  'update:modelValue': [value: PurchaseMode]
}>()

const { t } = useI18n()
const tablistRef = ref<HTMLDivElement | null>(null)
const activeTabIndex = computed(() => props.tabs.findIndex(tab => tab.key === props.modelValue))

function selectTab(key: PurchaseMode): void {
  if (key !== props.modelValue) {
    emit('update:modelValue', key)
  }
}

function focusTab(index: number): void {
  const tab = props.tabs[index]
  if (!tab) return

  selectTab(tab.key)
  void nextTick(() => {
    tablistRef.value
      ?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
      .item(index)
      .focus()
  })
}

function handleKeydown(event: KeyboardEvent, currentIndex: number): void {
  if (props.tabs.length === 0) return

  let nextIndex: number | null = null
  if (event.key === 'ArrowRight') {
    nextIndex = (currentIndex + 1) % props.tabs.length
  } else if (event.key === 'ArrowLeft') {
    nextIndex = (currentIndex - 1 + props.tabs.length) % props.tabs.length
  } else if (event.key === 'Home') {
    nextIndex = 0
  } else if (event.key === 'End') {
    nextIndex = props.tabs.length - 1
  }

  if (nextIndex === null) return
  event.preventDefault()
  focusTab(nextIndex)
}
</script>

<style scoped>
.purchase-mode-tabs {
  display: grid;
  width: 100%;
  min-height: 3.7rem;
  gap: 0.2rem;
  padding: 0.24rem;
  border: 1px solid rgb(15 23 42 / 16%);
  border-radius: 0.9rem;
  background: rgb(255 255 255 / 94%);
  box-shadow:
    0 0.5rem 1.5rem rgb(67 56 202 / 6%),
    inset 0 1px 0 rgb(255 255 255 / 95%);
  backdrop-filter: blur(18px);
}

.purchase-mode-tab {
  display: inline-flex;
  min-width: 0;
  min-height: 3.2rem;
  align-items: center;
  justify-content: center;
  gap: 0.55rem;
  padding: 0.75rem 1rem;
  border: 1px solid transparent;
  border-radius: 0.68rem;
  background: transparent;
  color: #475569;
  cursor: pointer;
  font-size: 0.95rem;
  font-weight: 750;
  line-height: 1.25;
  transition:
    color 180ms ease,
    background-color 180ms ease,
    border-color 180ms ease,
    box-shadow 180ms ease,
    transform 180ms ease;
}

.purchase-mode-tab:hover:not(.purchase-mode-tab--active) {
  border-color: rgb(99 102 241 / 16%);
  background: rgb(238 242 255 / 72%);
  color: #3730a3;
}

.purchase-mode-tab--active {
  border-color: #7457ff;
  background: rgb(255 255 255 / 98%);
  box-shadow:
    inset 0 0 0 3px rgb(116 87 255 / 20%),
    0 0.35rem 0.9rem rgb(79 70 229 / 14%);
  color: #5b45e8;
}

.purchase-mode-tab:active {
  transform: translateY(1px);
}

.purchase-mode-tab:focus-visible {
  outline: 3px solid rgb(14 165 233 / 72%);
  outline-offset: 3px;
}

.purchase-mode-tab__label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.purchase-mode-tab__badge {
  display: inline-flex;
  min-height: 1.3rem;
  align-items: center;
  padding: 0.1rem 0.42rem;
  border: 1px solid rgb(255 255 255 / 38%);
  border-radius: 999px;
  background: rgb(255 255 255 / 20%);
  color: inherit;
  font-size: 0.62rem;
  font-weight: 800;
  letter-spacing: 0.02em;
  white-space: nowrap;
}

.purchase-mode-tab:not(.purchase-mode-tab--active) .purchase-mode-tab__badge {
  border-color: rgb(99 102 241 / 20%);
  background: #eef2ff;
  color: #4f46e5;
}

:global(.dark) .purchase-mode-tabs {
  border-color: rgb(129 140 248 / 22%);
  background: rgb(30 32 52 / 88%);
  box-shadow:
    0 0.75rem 2rem rgb(0 0 0 / 22%),
    inset 0 1px 0 rgb(255 255 255 / 7%);
}

:global(.dark) .purchase-mode-tab {
  color: #cbd5e1;
}

:global(.dark) .purchase-mode-tab:hover:not(.purchase-mode-tab--active) {
  border-color: rgb(129 140 248 / 24%);
  background: rgb(79 70 229 / 14%);
  color: #e0e7ff;
}

:global(.dark) .purchase-mode-tab--active {
  border-color: #8b7dff;
  background: rgb(41 43 66 / 96%);
  box-shadow:
    inset 0 0 0 3px rgb(139 125 255 / 18%),
    0 0.35rem 0.9rem rgb(0 0 0 / 20%);
  color: #ddd6fe;
}

:global(.dark) .purchase-mode-tab:not(.purchase-mode-tab--active) .purchase-mode-tab__badge {
  border-color: rgb(129 140 248 / 25%);
  background: rgb(79 70 229 / 22%);
  color: #c7d2fe;
}

@media (max-width: 767px) {
  .purchase-mode-tabs {
    min-height: 3.75rem;
    gap: 0.3rem;
    padding: 0.35rem;
    border-radius: 1.15rem;
  }

  .purchase-mode-tab {
    min-height: 3.15rem;
    gap: 0.38rem;
    padding: 0.65rem 0.55rem;
    border-radius: 0.9rem;
    font-size: 0.88rem;
  }

  .purchase-mode-tab__badge {
    display: none;
  }
}

@media (max-width: 389px) {
  .purchase-mode-tab svg {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .purchase-mode-tab {
    transition: none;
  }
}
</style>
