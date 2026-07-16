// @ts-check
import { defineConfig, sessionDrivers } from "astro/config";
import starlight from "@astrojs/starlight";
import cloudflare from "@astrojs/cloudflare";
import starlightLlmsTxt from "starlight-llms-txt";
import starlightThemeBlack from "starlight-theme-black";

/** @returns {import("@astrojs/starlight/types").StarlightPlugin} */
function bastionHeaderLinks() {
  return {
    name: "bastion-header-links",
    hooks: {
      "config:setup": ({ config, updateConfig }) => {
        updateConfig({
          components: {
            ...config.components,
            Header: "./src/components/Header.astro",
          },
        });
      },
    },
  };
}

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
        starlightLlmsTxt(),
        starlightThemeBlack({
          footerText: `© ${new Date().getFullYear()} Bastion Computer. All rights reserved.`,
        }),
        bastionHeaderLinks(),
      ],
      favicon: "/favicon.ico",
      title: "bastion.computer",
      customCss: ["./src/styles/sidebar.css"],
      head: [
        {
          tag: "meta",
          attrs: {
            property: "og:image",
            content: "https://i.imgur.com/ohVUfp0.png",
          },
        },
        {
          tag: "meta",
          attrs: {
            name: "twitter:image",
            content: "https://i.imgur.com/ohVUfp0.png",
          },
        },
      ],
      social: [
        {
          icon: "email",
          label: "Email",
          href: "mailto:hazim@bastion.computer",
        },
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
          label: "Agents",
          items: [
            {
              label: "llms.txt",
              link: "/llms.txt",
              attrs: { target: "_blank", rel: "noopener noreferrer" },
            },
            {
              label: "llms-full.txt",
              link: "/llms-full.txt",
              attrs: { target: "_blank", rel: "noopener noreferrer" },
            },
            {
              label: "llms-small.txt",
              link: "/llms-small.txt",
              attrs: { target: "_blank", rel: "noopener noreferrer" },
            },
          ],
        },
        {
          label: "Guides",
          items: [
            { label: "System Setup", slug: "guides/system" },
            { label: "Base", slug: "guides/base" },
            { label: "Templates", slug: "guides/templates" },
            { label: "Environments", slug: "guides/environments" },
            { label: "Cluster", slug: "guides/cluster" },
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
        {
          label: "Examples",
          items: [
            {
              label: "Cluster with Docker Compose",
              slug: "examples/cluster-with-docker-compose",
            },
            {
              label: "Issue tracker demo",
              slug: "examples/bastion-demo-repo",
            },
            {
              label: "Remote access with Tailscale",
              slug: "examples/remote-access-with-tailscale",
            },
          ],
        },
      ],
    }),
  ],
});
