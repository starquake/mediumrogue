import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

// Build output lands inside the Go module so `go:embed all:dist` bundles the
// client into the server binary (see internal/web/web.go). The dev server
// proxies /api to a locally running Go server; SSE passes through the proxy
// unbuffered.
export default defineConfig({
  plugins: [solid()],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8080",
      },
    },
  },
});
