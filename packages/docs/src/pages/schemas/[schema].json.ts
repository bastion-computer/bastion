import type { APIRoute, GetStaticPaths } from "astro";
import action from "@bastion/spec/data-types/action.json";
import template from "@bastion/spec/data-types/template.json";

const schemas = {
  action,
  template,
} as const;

export const getStaticPaths = (() =>
  Object.keys(schemas).map((schema) => ({
    params: { schema },
  }))) satisfies GetStaticPaths;

export const GET: APIRoute = ({ params }) => {
  const schema = schemas[params.schema as keyof typeof schemas];

  if (!schema) {
    return new Response(null, { status: 404 });
  }

  return new Response(`${JSON.stringify(schema, null, 2)}\n`, {
    headers: { "Content-Type": "application/json" },
  });
};
