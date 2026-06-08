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
          label: "Reference",
          items: [
            { label: "CLI", slug: "reference/cli" },
            { label: "API", slug: "reference/api" },
            { label: "Configuration", slug: "reference/configuration" },
          ],
        },
        {
          label: "Templates",
          items: [
            {
              label: "Schema",
              link: "/schemas/template.json",
              attrs: { target: "_blank", rel: "noopener noreferrer" },
            },
            {
              label: "Bastion dev env",
              slug: "template-examples/bastion-dev-environment",
            },
          ],
        },
        {
          label: "Actions",
          items: [
            { label: "Custom Actions", slug: "actions/custom-actions" },
            {
              label: "Utility Tools",
              slug: "actions/built-ins/utility-tools",
            },
            { label: "Runtimes", slug: "actions/built-ins/runtimes" },
          ],
        },
      ],
    }),
  ],
});
