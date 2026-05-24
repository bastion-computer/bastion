/// <reference path="./.sst/platform/config.d.ts" />

export default $config({
  app(input) {
    return {
      name: "bastion",
      home: "cloudflare",
      removal: input.stage === "production" ? "retain" : "remove",
      protect: input.stage === "production",
    };
  },
  async run() {
    const docs = new sst.cloudflare.Astro("Docs", {
      path: "docs",
      buildCommand: "bun run build",
      dev: {
        command: "bun run dev",
        url: "http://localhost:4321",
      },
      domain: {
        name: "bastion.computer",
        redirects: ["www.bastion.computer"],
      },
    });

    return {
      docs: docs.url,
    };
  },
});
