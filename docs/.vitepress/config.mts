import { defineConfig } from "vitepress";

export default defineConfig({
  title: "Tau",
  description:
    "A provider-agnostic, OpenAI-compatible coding agent with a terminal UI, Web UI, tool use, skills, and plugins.",
  lastUpdated: true,
  cleanUrls: true,
  srcDir: ".",
  outDir: ".vitepress/dist",

  // Source files that document tau's own file formats (e.g. README.md,
  // which is also embedded into the tau binary for the agent's self-doc
  // tool) keep their filenames on disk but render at friendlier routes.
  rewrites: {
    "README.md": "cli-reference.md",
  },

  head: [["link", { rel: "icon", href: "/favicon.svg" }]],

  themeConfig: {
    logo: "/favicon.svg",

    nav: [
      { text: "Guide", link: "/cli-reference" },
      { text: "Architecture", link: "/architecture" },
      {
        text: "Extensions",
        items: [
          { text: "Plugin SDK", link: "/plugins" },
          {
            text: "Examples",
            items: [
              { text: "Creating an MCP Plugin", link: "/examples/mcp-plugin" },
              { text: "Advanced Plugins: Lifecycle Hooks & Prompts", link: "/examples/advanced-plugin" },
              { text: "Building a Custom Tool Plugin", link: "/examples/tool-plugin" },
            ],
          },
        ],
      },
      {
        text: "Changelog",
        link: "https://github.com/samcharles93/tau/blob/main/CHANGELOG.md",
      },
    ],

    sidebar: [
      {
        text: "Getting Started",
        items: [
          { text: "CLI Reference", link: "/cli-reference" },
          { text: "Configuration", link: "/configuration" },
          { text: "Providers", link: "/providers" },
        ],
      },
      {
        text: "Core Concepts",
        items: [
          { text: "Architecture", link: "/architecture" },
          { text: "Agent & Tool Loop", link: "/agent" },
          { text: "Built-in Tools", link: "/tools" },
          { text: "Skills", link: "/skills" },
          { text: "Plugin SDK", link: "/plugins" },
          { text: "Slash Commands", link: "/commands" },
          { text: "Sessions", link: "/sessions" },
        ],
      },
      {
        text: "Internals",
        items: [
          { text: "Event Bus", link: "/eventbus" },
          { text: "Chat Event/Command Types", link: "/chat-types" },
          { text: "Terminal UI Toolkit (taui)", link: "/taui" },
          { text: "TUI Implementation", link: "/tui" },
          { text: "HTTP Server", link: "/server" },
          { text: "AI SDK Integration", link: "/ai-sdk" },
        ],
      },
      {
        text: "Web UI",
        items: [
          { text: "Implementation", link: "/webui" },
          { text: "Technical Spec", link: "/specs/web-ui" },
          {
            text: "Protocol (AsyncAPI)",
            link: "https://github.com/samcharles93/tau/blob/main/docs/asyncapi/tau.yaml",
          },
        ],
      },
      {
        text: "Plugin Examples",
        items: [
          { text: "Creating an MCP Plugin", link: "/examples/mcp-plugin" },
          { text: "Advanced Plugins: Lifecycle Hooks & Prompts", link: "/examples/advanced-plugin" },
          { text: "Building a Custom Tool Plugin", link: "/examples/tool-plugin" },
        ],
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/samcharles93/tau" },
    ],

    search: {
      provider: "local",
    },

    editLink: {
      pattern: "https://github.com/samcharles93/tau/edit/main/docs/:path",
      text: "Edit this page on GitHub",
    },

    footer: {
      message: "Built with VitePress.",
      copyright: "Tau",
    },
  },
});
