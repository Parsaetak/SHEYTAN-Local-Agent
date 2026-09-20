import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8765",
        changeOrigin: false,
      },
      "/ws": {
        target: "ws://127.0.0.1:8765",
        ws: true,
        changeOrigin: false,
      },
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 4173,
    strictPort: true,
  },
  build: {
    target: "es2022",
    sourcemap: mode !== "production",
    // Manifest generation is disabled — no runtime consumer exists (the
    // embedded index.html references the stable assets directly; the
    // static-asset contract is verified by scripts/verify-static-assets.mjs).
    manifest: false,
    rollupOptions: {
      output: {
        // STABLE FILENAME CONTRACT: production assets are named after
        // their source module with NO content hash (index.js,
        // AgentBody.js, index.css, ...). The same physical file keeps the
        // same name across builds, so the embedded tree can be audited,
        // diffed and navigated without rebuilding. Code splitting stays
        // enabled — every lazy chunk is also stable.
        entryFileNames: "assets/[name].js",
        chunkFileNames: "assets/[name].js",
        assetFileNames: "assets/[name][extname]",
      },
    },
  },
}));
