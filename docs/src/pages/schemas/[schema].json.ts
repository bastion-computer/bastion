import type { APIRoute, GetStaticPaths } from "astro";
import templateSchema from "../../../../core/internal/schema/template.json?raw";

const schemas = {
  template: templateSchema,
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

  return new Response(schema.endsWith("\n") ? schema : `${schema}\n`, {
    headers: { "Content-Type": "application/json" },
  });
};
