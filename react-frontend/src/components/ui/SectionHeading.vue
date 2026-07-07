<script setup lang="ts">
withDefaults(
  defineProps<{
    eyebrow: string
    title: string
    description?: string
    align?: 'left' | 'center'
    tone?: 'light' | 'dark'
  }>(),
  {
    align: 'left',
    tone: 'light',
  },
)
</script>

<template>
  <div
    class="section-heading"
    :class="[
      align === 'center' ? 'mx-auto text-center' : '',
      tone === 'dark' ? 'section-heading--dark' : 'section-heading--light',
    ]"
  >
    <p class="section-heading__eyebrow">{{ eyebrow }}</p>
    <h2 class="section-heading__title text-balance">{{ title }}</h2>
    <p v-if="description" class="section-heading__description text-pretty">
      {{ description }}
    </p>
  </div>
</template>

<style scoped>
.section-heading {
  max-width: 44rem;
}

.section-heading__eyebrow {
  margin: 0;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.15em;
  text-transform: uppercase;
}

.section-heading__title {
  margin: 1rem 0 0;
  font-size: clamp(2.2rem, 4.8vw, 4.7rem);
  font-weight: 700;
  line-height: 1.04;
  letter-spacing: 0;
}

.section-heading__description {
  margin: 1.25rem 0 0;
  max-width: 38rem;
  font-size: clamp(1rem, 1.4vw, 1.12rem);
  line-height: 1.9;
}

.section-heading.mx-auto .section-heading__description {
  margin-inline: auto;
}

.section-heading--light .section-heading__eyebrow {
  color: rgba(26, 23, 20, 0.54);
}

.section-heading--light .section-heading__title {
  color: #0f0e0c;
}

.section-heading--light .section-heading__description {
  color: rgba(26, 23, 20, 0.66);
}

.section-heading--dark .section-heading__eyebrow {
  color: rgba(255, 249, 239, 0.58);
}

.section-heading--dark .section-heading__title {
  color: #fff9ef;
}

.section-heading--dark .section-heading__description {
  color: rgba(255, 249, 239, 0.68);
}
</style>
