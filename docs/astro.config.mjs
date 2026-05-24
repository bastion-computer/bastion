// @ts-check
import { defineConfig, sessionDrivers } from "astro/config";
import starlight from "@astrojs/starlight";
import cloudflare from "@astrojs/cloudflare";
import starlightThemeBlack from "starlight-theme-black";

// https://astro.build/config
export default defineConfig({
  site: "https://bastion.computer",
  session: {
    driver: sessionDrivers.lruCache(),
  },
  adapter: cloudflare({
    configPath: process.env.SST_WRANGLER_PATH,
    imageService: "compile",
    prerenderEnvironment: "node",
  }),
  integrations: [
    starlight({
      plugins: [
        starlightThemeBlack({
          footerText: `© ${new Date().getFullYear()} Bastion Computer. All rights reserved.`,
        }),
      ],
      favicon: "/favicon.ico",
      title: "bastion.computer",
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/bastion-computer/bastion",
        },
      ],
      logo: {
        light: "./src/assets/logo-light.png",
        dark: "./src/assets/logo-dark.png",
      },
      sidebar: [
        { label: "Introduction", slug: "introduction" },
        { label: "Quick Start", slug: "quick-start" },
        {
          label: "Guides",
          items: [
            { label: "System Setup", slug: "guides/system" },
            { label: "Templates", slug: "guides/templates" },
            { label: "Environments", slug: "guides/environments" },
            { label: "SSH", slug: "guides/ssh" },
          ],
        },
        {
          label: "Ecosystem",
          items: [
            { label: "Custom Actions", slug: "ecosystem/custom-actions" },
            {
              label: "Coding Agents",
              slug: "ecosystem/built-ins/coding-agents",
            },
            {
              label: "Utility Tools",
              slug: "ecosystem/built-ins/utility-tools",
            },
            { label: "Runtimes", slug: "ecosystem/built-ins/runtimes" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "CLI", slug: "reference/cli" },
            { label: "API", slug: "reference/api" },
            { label: "Configuration", slug: "reference/configuration" },
          ],
        },
        {
          label: "Schemas",
          items: [
            {
              label: "Template",
              link: "/schemas/template.json",
              attrs: { target: "_blank", rel: "noopener noreferrer" },
            },
          ],
        },
      ],
    }),
  ],
});
