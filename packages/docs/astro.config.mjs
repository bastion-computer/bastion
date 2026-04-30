// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import starlightThemeBlack from "starlight-theme-black";

// https://astro.build/config
export default defineConfig({
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
          items: [{ label: "Example Guide", slug: "guides/example" }],
        },
        {
          label: "Reference",
          autogenerate: { directory: "reference" },
        },
      ],
    }),
  ],
});
