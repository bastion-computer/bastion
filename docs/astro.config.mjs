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
      title: "Bastion",
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
        {
          label: "Tutorials",
          items: [
            {
              label: "Get started",
              slug: "tutorials/get-started",
            },
            {
              label: "Run parallel agents",
              slug: "tutorials/run-parallel-agents",
            },
          ],
        },
        {
          label: "How-to guides",
          items: [
            {
              label: "Install, update, or remove Bastion",
              slug: "how-to/install-update-remove",
            },
            { label: "Manage the base", slug: "how-to/manage-base" },
            { label: "Manage secrets", slug: "how-to/manage-secrets" },
            {
              label: "Create and manage templates",
              slug: "how-to/create-manage-templates",
            },
            {
              label: "Create custom actions",
              slug: "how-to/create-custom-actions",
            },
            {
              label: "Manage environments",
              slug: "how-to/manage-environments",
            },
            {
              label: "Connect to environments",
              slug: "how-to/connect-to-environments",
            },
            {
              label: "Expose environment services",
              slug: "how-to/expose-environment-services",
            },
            {
              label: "Back up and restore",
              slug: "how-to/back-up-and-restore",
            },
            {
              label: "Deploy and operate a cluster",
              slug: "how-to/deploy-and-operate-cluster",
            },
            {
              label: "Access Bastion with Tailscale",
              slug: "how-to/remote-access-with-tailscale",
            },
            {
              label: "Run a cluster with Docker Compose",
              slug: "how-to/run-cluster-with-docker-compose",
            },
            { label: "Troubleshoot Bastion", slug: "how-to/troubleshoot" },
          ],
        },
        {
          label: "Reference",
          items: [
            {
              label: "Host requirements and configuration",
              slug: "reference/host-requirements-and-configuration",
            },
            {
              label: "Template configuration",
              slug: "reference/template-configuration",
            },
            {
              label: "Action manifest",
              slug: "reference/action-manifest",
            },
            {
              label: "Built-in actions",
              slug: "reference/built-in-actions",
            },
            {
              label: "Environment states and streams",
              slug: "reference/environment-states-and-streams",
            },
            {
              label: "CLI",
              items: [
                { label: "Host commands", slug: "reference/cli/host" },
                { label: "Cluster commands", slug: "reference/cli/cluster" },
              ],
            },
            {
              label: "API",
              items: [
                { label: "Host API", slug: "reference/api/host" },
                { label: "Cluster API", slug: "reference/api/cluster" },
              ],
            },
            { label: "Glossary", slug: "reference/glossary" },
          ],
        },
        {
          label: "Explanation",
          items: [
            {
              label: "How Bastion works",
              slug: "explanation/how-bastion-works",
            },
            {
              label: "Resource lifecycle",
              slug: "explanation/resource-lifecycle",
            },
            {
              label: "Actions and secrets",
              slug: "explanation/actions-and-secrets",
            },
            {
              label: "Clusters and namespaces",
              slug: "explanation/clusters-and-namespaces",
            },
            {
              label: "Security and operational limits",
              slug: "explanation/security-and-operational-limits",
            },
          ],
        },
      ],
    }),
  ],
});
