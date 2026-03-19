import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Vite configuration for the Axiom GUI dashboard.
// In production, Wails embeds the built frontend assets into the Go binary.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
  },
  server: {
    port: 5173,
  },
});
