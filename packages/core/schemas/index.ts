const schemaModules = import.meta.glob("./*.json", {
  eager: true,
  import: "default",
});

export const schemas = Object.fromEntries(
  Object.entries(schemaModules).flatMap(([path, schema]) => {
    const match = path.match(/\/([^/]+)\.json$/);

    return match ? [[match[1], schema] as const] : [];
  }),
) as Record<string, unknown>;
