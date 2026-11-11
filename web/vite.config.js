import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  base: "/console/",
  build: {
    manifest: true,
    rollupOptions: {
      input: {
        console: fileURLToPath(new URL("./index.html", import.meta.url)),
        auth: fileURLToPath(new URL("./src/auth.ts", import.meta.url)),
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8900",
      "/login": "http://127.0.0.1:8900",
      "/device": "http://127.0.0.1:8900",
      "/.well-known": "http://127.0.0.1:8900",
    },
  },
});
