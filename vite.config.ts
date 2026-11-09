import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    // Component tests mount Svelte using the browser entry in jsdom.
    conditions: ["browser"],
  },
  build: {
    rolldownOptions: {
      input: ["bubble.html", "settings.html"],
    },
  },
  server: {
    // Keep this port in sync with devUrl in mygo.config.ts.
    port: 5173,
    strictPort: true,
    // The development app and the packaged builds are not the page's.
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
});
