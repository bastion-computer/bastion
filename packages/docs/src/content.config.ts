import { defineCollection } from "astro:content";
import { z } from "astro/zod";
import { docsLoader } from "@astrojs/starlight/loaders";
import { docsSchema } from "@astrojs/starlight/schema";

const previewBanner =
  "Bastion v0.1 is under development. These docs are available as a public preview.";

export const collections = {
  docs: defineCollection({
    loader: docsLoader(),
    schema: docsSchema({
      extend: z.object({
        banner: z.object({ content: z.string() }).default({
          content: previewBanner,
        }),
      }),
    }),
  }),
};
