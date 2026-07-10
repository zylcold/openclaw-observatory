import { defineConfig } from "vite";

export default defineConfig({
  base: "/",
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:10087",
      "/health": "http://127.0.0.1:10087",
      "/ready": "http://127.0.0.1:10087",
      "/metrics": "http://127.0.0.1:10087",
    },
  },
});
