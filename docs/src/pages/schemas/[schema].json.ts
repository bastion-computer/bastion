import type { APIRoute, GetStaticPaths } from "astro";
import { readFile } from "node:fs/promises";

const schemas = {
  template: new URL(
    "../../../../core/internal/schema/template.json",
    import.meta.url,
  ),
} as const;

export const getStaticPaths = (() =>
  Object.keys(schemas).map((schema) => ({
    params: { schema },
  }))) satisfies GetStaticPaths;

export const GET: APIRoute = async ({ params }) => {
  const schema = schemas[params.schema as keyof typeof schemas];

  if (!schema) {
    return new Response(null, { status: 404 });
  }

  const contents = await readFile(schema, "utf8");

  return new Response(contents.endsWith("\n") ? contents : `${contents}\n`, {
    headers: { "Content-Type": "application/json" },
  });
};
