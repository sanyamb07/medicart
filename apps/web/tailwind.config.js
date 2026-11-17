/** @type {import('tailwindcss').Config} */
//
// MediCart design tokens.
//
// brand   — deep clinical teal: primary actions, links, focus rings.
// ink     — warm-cool neutral used for text and surfaces (slate is kept for
//           existing admin/platform screens, which still reference it).
// save    — reserved for savings and "in stock" only; never decorative.
// rx      — amber, reserved for prescription requirements and cautions.
//
// `teal` is aliased to brand so older screens that still say `teal-600`
// render in the same hue instead of a near-duplicate.
const brand = {
  50: "#eefaf7",
  100: "#d4f1e9",
  200: "#abe3d4",
  300: "#76ccb9",
  400: "#40ad99",
  500: "#1f917f",
  600: "#127567",
  700: "#105f55",
  800: "#114d46",
  900: "#11403b",
  950: "#062521",
};

export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    screens: {
      sm: "640px",
      md: "768px",
      lg: "1024px",
      xl: "1280px",
    },
    extend: {
      colors: {
        brand,
        teal: brand,
        ink: {
          50: "#f7f8f9",
          100: "#eef0f2",
          200: "#dfe3e7",
          300: "#c4cbd2",
          400: "#97a1ab",
          500: "#6a7682",
          600: "#4d5864",
          700: "#38424d",
          800: "#232c35",
          900: "#141b22",
        },
        save: { 50: "#ecfdf3", 100: "#d1fadf", 600: "#079455", 700: "#067647" },
        rx: { 50: "#fffaeb", 100: "#fef0c7", 200: "#fedf89", 600: "#b54708", 700: "#93370d", 800: "#7a2e0e" },
        canvas: "#f6f7f5",
      },
      fontFamily: {
        sans: ["Inter", "system-ui", "-apple-system", "Segoe UI", "Roboto", "sans-serif"],
        display: ['"Plus Jakarta Sans"', "Inter", "system-ui", "sans-serif"],
      },
      fontSize: {
        // 12px is the floor for anything a customer needs to read.
        "2xs": ["0.6875rem", { lineHeight: "1rem" }],
      },
      boxShadow: {
        card: "0 1px 2px rgba(20,27,34,0.04), 0 1px 3px rgba(20,27,34,0.06)",
        cardHover: "0 12px 24px -8px rgba(20,27,34,0.14), 0 4px 8px -4px rgba(20,27,34,0.08)",
        pop: "0 24px 48px -12px rgba(20,27,34,0.22), 0 8px 16px -8px rgba(20,27,34,0.1)",
        search: "0 2px 4px rgba(20,27,34,0.04), 0 8px 24px -6px rgba(17,77,70,0.16)",
        bar: "0 -6px 20px -6px rgba(20,27,34,0.12)",
      },
      borderRadius: {
        xl2: "1.25rem",
        "4xl": "2rem",
      },
      maxWidth: {
        page: "80rem",
      },
      keyframes: {
        "fade-in": { from: { opacity: "0" }, to: { opacity: "1" } },
        "pop-in": {
          from: { opacity: "0", transform: "translateY(-4px) scale(0.98)" },
          to: { opacity: "1", transform: "translateY(0) scale(1)" },
        },
        "slide-up": {
          from: { opacity: "0", transform: "translateY(12px)" },
          to: { opacity: "1", transform: "translateY(0)" },
        },
        "sheet-up": { from: { transform: "translateY(100%)" }, to: { transform: "translateY(0)" } },
        bump: {
          "0%,100%": { transform: "scale(1)" },
          "40%": { transform: "scale(1.25)" },
        },
        shimmer: { "100%": { transform: "translateX(100%)" } },
      },
      animation: {
        "fade-in": "fade-in 160ms ease-out",
        "pop-in": "pop-in 160ms cubic-bezier(0.2,0.8,0.2,1)",
        "slide-up": "slide-up 240ms cubic-bezier(0.2,0.8,0.2,1)",
        "sheet-up": "sheet-up 260ms cubic-bezier(0.2,0.8,0.2,1)",
        bump: "bump 360ms ease-out",
      },
    },
  },
  plugins: [],
};
