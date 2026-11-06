import { defineConfig } from "vite";

export default defineConfig({
  server: {
    // Keep this port in sync with devUrl in mygo.config.ts.
    port: 5173,
    strictPort: true,
    // The development app and the packaged builds are not the page's.
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
});
