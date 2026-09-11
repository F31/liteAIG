import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { loadEnv } from "vite";
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "");
  return {
    plugins: [react()],
    build: { outDir: "dist", emptyOutDir: true },
    test: {
      environment: "jsdom",
      setupFiles: ["./src/test/setup.ts"],
      exclude: ["e2e/**", "node_modules/**", "dist/**"],
    },
    server: {
      proxy: {
        "/api/admin": {
          target: env.LITEAIG_ADMIN_ADDR ?? "http://localhost:8081",
          changeOrigin: true,
        },
      },
    },
  };
});
