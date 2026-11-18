import { defineConfig } from "blume";
import { openapi } from "blume/reference";
import { filesystem } from "blume/sources";

export default defineConfig({
  title: "piecemeal",
  description:
    "A self-hosted BitTorrent client in Go: a qBittorrent-compatible API for Radarr, Sonarr and Prowlarr, a modern web UI, and SSO.",
  logo: { image: "./logo.svg", text: "piecemeal" },
  github: { owner: "sebdanielsson", repo: "piecemeal", branch: "main" },

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

  footer: {
    copyright: "© 2026 Sebastian Danielsson · MIT License",
    links: [
      { label: "Docs", href: "/docs" },
      { label: "API", href: "/reference" },
      { label: "Releases", href: "https://github.com/sebdanielsson/piecemeal/releases" },
    ],
  },
});
