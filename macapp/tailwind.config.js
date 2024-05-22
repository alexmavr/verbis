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
    themes: ["winter", "night"],
    darkTheme: "night",
  },
  variants: {
    opacity: ["disabled"],
    curson: ["disabled"],
  },
};
