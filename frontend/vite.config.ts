import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true
  },
  server: {
    port: 3000,
    headers: {
      "Cache-Control": "no-store"
    }
  }
});
