/// <reference types="vitest/config" />
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  // `pnpm dev` talks to a locally running piecemeal. PIECEMEAL_DEV_API points
  // it at another address, e.g. PIECEMEAL_DEV_API=http://127.0.0.1:18090.
  const backend = loadEnv(mode, ".", "PIECEMEAL_").PIECEMEAL_DEV_API || "http://localhost:8080";
  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: { "@": decodeURIComponent(new URL("./src", import.meta.url).pathname) },
    },
    build: {
      // The Go binary embeds this directory (internal/webui).
      outDir: "../internal/webui/dist",
      emptyOutDir: true,
      rolldownOptions: {
        output: {
          // Libraries change less often than the app, so they get their own
          // long-cached chunks.
          codeSplitting: {
            groups: [
              { name: "react", test: /node_modules[\\/](react|react-dom|scheduler)[\\/]/ },
              { name: "tanstack", test: /node_modules[\\/]@tanstack[\\/]/ },
            ],
          },
        },
      },
    },
    server: {
      // Keep the browser's Host header: the API refuses unsafe requests whose
      // Origin doesn't match it (CSRF), and the shorthand form rewrites it.
      proxy: {
        "/api/": { target: backend, changeOrigin: false },
        "/healthz": { target: backend, changeOrigin: false },
      },
    },
    test: {
      environment: "jsdom",
      setupFiles: ["./src/test/setup.ts"],
    },
  };
});
