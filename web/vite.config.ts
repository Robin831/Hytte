import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tailwindcss(),
    react(),
  ],
  define: {
    __BUILD_HASH__: JSON.stringify(Date.now().toString(36)),
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080'
    }
  },
  test: {
    setupFiles: ['./src/test-setup.ts'],
    // The Hetzner box has 7.6 GB RAM. vmThreads spawns multiple V8 isolates
    // that each balloon to ~4 GB under React 19, OOM-killing Forge smiths.
    // forks + singleFork + fileParallelism:false bounds the test run to one
    // child process (~1 GB peak) at the cost of running suites sequentially.
    pool: 'forks',
    poolOptions: {
      forks: {
        singleFork: true,
      },
    },
    fileParallelism: false,
    server: {
      deps: {
        inline: ['refractor'],
      },
    },
  },
})
