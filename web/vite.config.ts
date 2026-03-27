// Dev: terminal 1: cd web && npm run dev
//      terminal 2: AI_CHAT_DEV=1 go run ./cmd/ai-chat/ start
// Prod: cd web && npm run build && go build ./cmd/ai-chat/
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/ws": {
        target: "http://localhost:8080",
        ws: true,
      },
    },
  },
});
