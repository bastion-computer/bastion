// @ts-check
import { defineConfig, sessionDrivers } from "astro/config";
import starlight from "@astrojs/starlight";
import cloudflare from "@astrojs/cloudflare";
import starlightLlmsTxt from "starlight-llms-txt";
import starlightThemeBlack from "starlight-theme-black";
import starlightBlog from "starlight-blog";

// Both the theme and blog plugin replace MarkdownContent, so install a final bridge.
/** @type {import("@astrojs/starlight/types").StarlightPlugin} */
const markdownContentBridge = {
  name: "bastion-markdown-content-bridge",
  hooks: {
    "config:setup"({ config, updateConfig }) {
      updateConfig({
        components: {
          ...config.components,
          MarkdownContent: "./src/components/MarkdownContent.astro",
        },
      });
    },
  },
};

// https://astro.build/config
export default defineConfig({
  site: "https://bastion.computer",
  vite: {
    optimizeDeps: {
      exclude: ["starlight-blog"],
    },
  },
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
          navLinks: [{ label: "Blog", link: "/blog/" }],
        }),
        starlightBlog({
          authors: {
            hazim: {
              name: "Hazim",
              title: "Primary Maintainer",
            },
          },
          navigation: "none",
          prefix: "blog",
          title: "Blog",
        }),
        markdownContentBridge,
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
        { label: "Blog", link: "/blog/" },
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
        {
          label: "Examples",
          items: [
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
