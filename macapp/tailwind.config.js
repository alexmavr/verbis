/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/**/*.{js,ts,jsx,tsx,mdx}"],
  theme: {
    extend: {
      borderWidth: {
        1: "1px",
      },
    },
  },
  plugins: [require("daisyui")],
  daisyui: {
    themes: [
      {
        "verbis-light": {
          primary: "#f3f4f6",
          secondary: "#e5e7eb",
          accent: "#4b5563",
          neutral: "#9ca3af",
          "base-100": "#ffffff",
          info: "#3b82f6",
          success: "#10b981",
          warning: "#f59e0b",
          error: "#ef4444",
        },
      },
      ,
      "winter",
      "night",
    ],
    darkTheme: "night",
  },
  variants: {
    opacity: ["disabled"],
    curson: ["disabled"],
  },
};
