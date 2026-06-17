<template>
  <div class="space-y-4">
    <!-- Quick Amount Buttons -->
    <div>
      <label class="mb-2 block text-xs font-semibold uppercase tracking-[1px] text-gray-400 dark:text-gray-500">
        {{ t('payment.quickAmounts') }}
      </label>
      <div class="grid grid-cols-3 gap-2.5">
        <button
          v-for="amt in filteredAmounts"
          :key="amt"
          type="button"
          :class="[
            'rounded-xl px-4 py-3 text-center font-medium transition-all',
            modelValue === amt
              ? 'bg-[#E5EAFF] text-[#0033FF] shadow-[0_0_0_1.5px_#0033FF] dark:bg-primary-950/40 dark:text-primary-300 dark:shadow-[0_0_0_1.5px_theme(colors.primary.400)]'
              : 'bg-white text-gray-700 shadow-[0_0_0_1px_#E5EAFF] hover:shadow-[0_0_0_1px_#0033FF] dark:bg-dark-800 dark:text-gray-200 dark:shadow-[0_0_0_1px_rgba(255,255,255,0.1)]',
          ]"
          @click="selectAmount(amt)"
        >
          {{ amt }}
        </button>
      </div>
    </div>

    <!-- Custom Amount Input -->
    <div>
      <label class="mb-2 block text-xs font-semibold uppercase tracking-[1px] text-gray-400 dark:text-gray-500">
        {{ t('payment.customAmount') }}
      </label>
      <div class="relative">
        <span class="absolute left-4 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500">
          $
        </span>
        <input
          type="text"
          inputmode="decimal"
          :value="customText"
          :placeholder="placeholderText"
          class="w-full rounded-xl border-0 bg-white py-3 pl-8 pr-4 text-gray-900 shadow-[0_0_0_1px_#E5EAFF] transition-all placeholder:text-gray-400 focus:shadow-[0_0_0_1.5px_#0033FF] focus:outline-none dark:bg-dark-800 dark:text-gray-100 dark:shadow-[0_0_0_1px_rgba(255,255,255,0.1)]"
          @input="handleInput"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  amounts?: number[]
  modelValue: number | null
  min?: number
  max?: number
}>(), {
  amounts: () => [10, 20, 50, 100, 200, 500, 1000, 2000, 5000],
  min: 0,
  max: 0,
})

const emit = defineEmits<{
  'update:modelValue': [value: number | null]
}>()

const { t } = useI18n()

const customText = ref('')

// 0 = no limit
const filteredAmounts = computed(() =>
  props.amounts.filter((a) => (props.min <= 0 || a >= props.min) && (props.max <= 0 || a <= props.max))
)

const placeholderText = computed(() => {
  if (props.min > 0 && props.max > 0) return `${props.min} - ${props.max}`
  if (props.min > 0) return `≥ ${props.min}`
  if (props.max > 0) return `≤ ${props.max}`
  return t('payment.enterAmount')
})

const AMOUNT_PATTERN = /^\d*(\.\d{0,2})?$/

function selectAmount(amt: number) {
  customText.value = String(amt)
  emit('update:modelValue', amt)
}

function handleInput(e: Event) {
  const val = (e.target as HTMLInputElement).value
  if (!AMOUNT_PATTERN.test(val)) return
  customText.value = val
  if (val === '') {
    emit('update:modelValue', null)
    return
  }
  const num = parseFloat(val)
  if (!isNaN(num) && num > 0) {
    emit('update:modelValue', num)
  } else {
    emit('update:modelValue', null)
  }
}

watch(() => props.modelValue, (v) => {
  if (v !== null && String(v) !== customText.value) {
    customText.value = String(v)
  }
}, { immediate: true })
</script>
