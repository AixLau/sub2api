import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    assetsDir: 'landing-assets',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/react')) {
            return 'react-vendor'
          }

          if (id.includes('node_modules/gsap')) {
            return 'auth-vendor'
          }
        },
      },
    },
  },
})
