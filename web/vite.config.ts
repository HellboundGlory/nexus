/// <reference types="vitest/config" />
import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    // Dev-only: forward API + WebSocket to the Go backend (default port 9494,
    // from config.Addr(); override if you set NEXUS_PORT). `ws: true` also
    // proxies /api/v1/ws so the live activity feed works under HMR.
    proxy: {
      "/api": { target: "http://localhost:9494", changeOrigin: true, ws: true },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
})
