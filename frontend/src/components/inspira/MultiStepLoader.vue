<template>
  <ol class="msl-root space-y-1" role="list">
    <li
      v-for="(step, index) in steps"
      :key="index"
      class="msl-step flex items-start gap-3 rounded-md px-2 py-1.5 transition-all duration-300"
      :class="[`msl-${stateOf(index)}`, stateOf(index) === 'pending' ? 'opacity-60' : 'opacity-100']"
    >
      <span class="relative mt-0.5 flex h-5 w-5 flex-shrink-0 items-center justify-center">
        <!-- Completed: check with stroke draw animation -->
        <svg
          v-if="stateOf(index) === 'done'"
          class="msl-check h-5 w-5 text-teal-500 dark:text-teal-400"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path class="msl-check-path" d="M5 13l4 4L19 7" />
        </svg>
        <!-- Current + error: red cross -->
        <svg
          v-else-if="stateOf(index) === 'error'"
          class="msl-cross h-5 w-5 text-red-500 dark:text-red-400"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          stroke-linecap="round"
          aria-hidden="true"
        >
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
        <!-- Current: spinner -->
        <span
          v-else-if="stateOf(index) === 'active'"
          class="msl-spinner h-4 w-4 rounded-full border-2 border-teal-500 border-t-transparent dark:border-teal-400 dark:border-t-transparent"
          aria-hidden="true"
        ></span>
        <!-- Pending: gray dot -->
        <span
          v-else
          class="msl-dot h-2 w-2 rounded-full bg-gray-300 transition-colors duration-300 dark:bg-gray-600"
          aria-hidden="true"
        ></span>
      </span>
      <span class="min-w-0 flex-1">
        <span
          class="block text-sm font-medium transition-colors duration-300"
          :class="titleClass(index)"
        >
          {{ step.title }}
        </span>
        <span
          v-if="step.description"
          class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400"
        >
          {{ step.description }}
        </span>
      </span>
    </li>
  </ol>
</template>

<script setup lang="ts">
export interface MultiStepLoaderStep {
  title: string
  description?: string
}

interface Props {
  steps: MultiStepLoaderStep[]
  /** 0-based index of the step currently in progress */
  current: number
  /** Whether the current step has failed */
  error?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  error: false
})

type StepState = 'done' | 'active' | 'error' | 'pending'

function stateOf(index: number): StepState {
  if (index < props.current) return 'done'
  if (index === props.current) return props.error ? 'error' : 'active'
  return 'pending'
}

function titleClass(index: number): string {
  const state = stateOf(index)
  if (state === 'done') return 'text-teal-600 dark:text-teal-400'
  if (state === 'error') return 'text-red-600 dark:text-red-400'
  if (state === 'active') return 'text-gray-900 dark:text-gray-100'
  return 'text-gray-400 dark:text-gray-500'
}
</script>

<style scoped>
.msl-check-path {
  stroke-dasharray: 24;
  stroke-dashoffset: 0;
  animation: msl-draw 0.4s ease-out backwards;
}

.msl-spinner {
  animation: msl-spin 0.8s linear infinite;
}

.msl-cross {
  animation: msl-pop 0.25s ease-out backwards;
}

@keyframes msl-draw {
  from {
    stroke-dashoffset: 24;
  }
  to {
    stroke-dashoffset: 0;
  }
}

@keyframes msl-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes msl-pop {
  from {
    transform: scale(0.5);
    opacity: 0;
  }
  to {
    transform: scale(1);
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .msl-check-path,
  .msl-spinner,
  .msl-cross,
  .msl-step {
    animation: none !important;
    transition: none !important;
  }
}
</style>
