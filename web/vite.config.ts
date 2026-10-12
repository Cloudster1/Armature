// vitest/config re-exports Vite's defineConfig with the `test` block typed,
// which keeps one config file instead of two that can drift apart.
import { configDefaults, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

/** Tests that stand the browser in another time zone, see src/test/zone.ts. */
const ZONE_TESTS = "src/**/*.zone.test.{ts,tsx}";

export default defineConfig({
  // Tests never load the stylesheet, so Tailwind would only add start-up time.
  plugins: process.env.VITEST ? [react()] : [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    // Vite rejects requests whose Host header it does not recognise, which
    // blocks anything reaching the dev server by its compose service name.
    // Development only; the production build is static files behind nginx.
    allowedHosts: ["localhost", "127.0.0.1", "web"],
    // The API runs on its own origin in development. Proxying it here means the
    // browser sees one origin, so the session cookie is first-party and no CORS
    // preflight is involved.
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY ?? "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    // Worker threads start faster than child processes and each file still gets
    // its own jsdom, but a thread ignores TZ, so tests that change it get a process.
    projects: [
      { extends: true, test: { name: "threads", pool: "threads", exclude: [...configDefaults.exclude, ZONE_TESTS] } },
      { extends: true, test: { name: "zone", pool: "forks", include: [ZONE_TESTS] } },
    ],
  },
});
