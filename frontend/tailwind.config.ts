import type { Config } from "tailwindcss";

export default {
  content: ["./src/**/*.{ts,tsx}", "./index.html"],
  theme: {
    extend: {
      fontFamily: {
        mono: ['"JetBrains Mono"', '"Fira Code"', "Consolas", "monospace"],
      },
      keyframes: {
        "alert-strobe-amber": {
          "0%, 100%": { borderLeftColor: "rgb(217 119 6 / 0.6)" },
          "50%": { borderLeftColor: "rgb(217 119 6 / 1)" },
        },
        "alert-strobe-rose": {
          "0%, 100%": { borderLeftColor: "rgb(225 29 72 / 0.6)" },
          "50%": { borderLeftColor: "rgb(225 29 72 / 1)" },
        },
        "alert-flash": {
          "0%, 100%": { opacity: "1" },
          "50%": { opacity: "0.7" },
        },
        "pulse-ring": {
          "0%": { boxShadow: "0 0 0 0 rgba(251 191 36 / 0.4)" },
          "70%": { boxShadow: "0 0 0 4px rgba(251 191 36 / 0)" },
          "100%": { boxShadow: "0 0 0 0 rgba(251 191 36 / 0)" },
        },
        "toast-slide-in": {
          "0%": { transform: "translateX(100%)", opacity: "0" },
          "100%": { transform: "translateX(0)", opacity: "1" },
        },
      },
      animation: {
        "alert-strobe-amber": "alert-strobe-amber 1.5s ease-in-out infinite",
        "alert-strobe-rose": "alert-strobe-rose 1.5s ease-in-out infinite",
        "alert-flash": "alert-flash 2s ease-in-out infinite",
        "pulse-ring": "pulse-ring 1.5s ease-out infinite",
        "toast-slide-in": "toast-slide-in 0.3s ease-out",
      },
    },
  },
  plugins: [],
} satisfies Config;
