/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  build: {
    // The Go binary embeds this directory (internal/webui).
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  server: {
    // `pnpm dev` talks to a locally running piecemeal.
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
  test: {
    environment: "jsdom",
  },
});
