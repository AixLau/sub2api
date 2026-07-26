<template>
  <span class="glitch relative inline-block" :data-text="text">{{ text }}</span>
</template>

<script setup lang="ts">
defineProps<{ text: string }>()
</script>

<style scoped>
.glitch::before,
.glitch::after {
  content: attr(data-text);
  position: absolute;
  inset: 0;
  overflow: hidden;
  color: inherit;
}

.glitch::before {
  left: 2px;
  text-shadow: -2px 0 #06b6d4;
  animation: glitch-a 2.5s infinite linear alternate-reverse;
}

.glitch::after {
  left: -2px;
  text-shadow: 2px 0 #ec4899;
  animation: glitch-b 3s infinite linear alternate-reverse;
}

@keyframes glitch-a {
  0% {
    clip-path: inset(20% 0 60% 0);
  }
  20% {
    clip-path: inset(80% 0 5% 0);
  }
  40% {
    clip-path: inset(35% 0 45% 0);
  }
  60% {
    clip-path: inset(60% 0 15% 0);
  }
  80% {
    clip-path: inset(10% 0 75% 0);
  }
  100% {
    clip-path: inset(45% 0 30% 0);
  }
}

@keyframes glitch-b {
  0% {
    clip-path: inset(65% 0 10% 0);
  }
  20% {
    clip-path: inset(15% 0 70% 0);
  }
  40% {
    clip-path: inset(55% 0 25% 0);
  }
  60% {
    clip-path: inset(5% 0 85% 0);
  }
  80% {
    clip-path: inset(70% 0 8% 0);
  }
  100% {
    clip-path: inset(30% 0 50% 0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .glitch::before,
  .glitch::after {
    content: none;
    animation: none;
  }
}
</style>
