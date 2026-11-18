import { defineConfig } from "blume";
import { openapi } from "blume/reference";
import { filesystem } from "blume/sources";

export default defineConfig({
  title: "piecemeal",
  description:
    "A self-hosted BitTorrent client in Go: a qBittorrent-compatible API for Radarr, Sonarr and Prowlarr, a modern web UI, and SSO.",
  logo: { image: "./logo.svg", text: "piecemeal" },
  github: { owner: "sebdanielsson", repo: "piecemeal", branch: "main" },
  // Explicit, so local builds produce the same canonical URLs, sitemap and
  // Open Graph cards as Vercel's.
  deployment: { site: "https://piecemeal-flax.vercel.app" },

  // The docs are the Markdown files in the repository's docs/ folder, so
  // they read the same on GitHub. plan.md is the internal design plan.
  content: {
    sources: [filesystem({ root: "../docs", prefix: "docs", exclude: ["plan.md"] })],
  },

  // The API reference is generated from the committed OpenAPI document; the
  // overlay only adds a public placeholder server for the code samples.
  reference: [
    openapi({
      route: "/reference",
      spec: "../api/openapi.json",
      overlays: ["./overlays/public.yaml"],
      playground: false,
      codeSamples: ["curl", "js", "python", "go"],
    }),
  ],

  theme: {
    // The web UI's palette: its seeding green, and its warm near-black.
    accent: { light: "oklch(0.56 0.13 155)", dark: "oklch(0.72 0.13 155)" },
    background: { dark: "oklch(0.147 0.004 49.3)" },
    radius: "none",
    mode: "system",
    fonts: {
      display: "playfair-display",
      body: { name: "Noto Sans", weights: [400, 500, 600, 700] },
      mono: "jetbrains-mono",
    },
  },

  navigation: {
    tabs: [
      { label: "Docs", path: "/docs" },
      { label: "API", path: "/reference" },
    ],
  },

  markdown: { externalLinks: true },

  // Docs pages get generated cards in the site's dark palette; the home
  // page has its own, og/home.html rendered to public/og-home.png.
  seo: {
    og: {
      logo: "/og-logo.svg",
      palette: {
        accent: "oklch(0.72 0.13 155)",
        background: "oklch(0.147 0.004 49.3)",
        foreground: "#fafaf9",
        muted: "#a8a29e",
        border: "#292524",
      },
    },
  },

  footer: {
    copyright: "© 2026 Sebastian Danielsson · MIT License",
    links: [
      { label: "Docs", href: "/docs" },
      { label: "API", href: "/reference" },
      { label: "Releases", href: "https://github.com/sebdanielsson/piecemeal/releases" },
    ],
  },
});
