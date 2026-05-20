/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // CSP-flavored sky blue, centered on #0091e1 (matches AppShell --accent).
        brand: {
          50: "#e6f4fc",
          100: "#cce9f8",
          200: "#99d2f1",
          300: "#66bce9",
          400: "#33a5e2",
          500: "#0091e1",
          600: "#0074b4",
          700: "#005784",
          800: "#003a59",
          900: "#001d2d",
          950: "#001120",
        },
      },
    },
  },
  plugins: [],
};
