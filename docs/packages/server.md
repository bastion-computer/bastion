This package contains the primary API layer built with [Hono](https://hono.dev/llms.txt) using Bun server as the runtime.

Additional references:

- https://bun.com/docs/runtime/http/server
- https://bun.com/docs/guides/ecosystem/hono
- https://hono.dev/docs/getting-started/bun

## Entrypoint

`src/index.ts` serves as the main entrypoint for the server, with the following structure:

```ts
import { Hono } from "hono";
import { HTTPException } from "hono/http-exception";
import { exampleApi } from "./example";

const app = new Hono()
  .onError((err, c) => {
    console.error(err);

    if (err instanceof HTTPException) {
      return err.getResponse();
    }
    return c.json({ error: "Internal server error" }, 500);
  })
  .route("/v1/example", exampleApi);

export { app };
export type AppType = typeof app;
```

- All routes MUST be grouped by a version and its core module. See [core.md](./core.md) for details.
- `AppType` export is ESSENTIAL for strong typing via the hono rpc client. This client will be used by downstream packages to interface with the API.

## Module API

Module APIs are located in their respective `src/$MODULE_NAME` directory (e.g. `src/example`).

### Route file

Route logic for the domain will be in the `index.ts` file (i.e. `src/$MODULE_NAME/index.ts`) and contain all the CRUD routes to match the underlying module.

```ts
import { Example } from "@bastion/core/example";
import { sValidator } from "@hono/standard-validator";
import { Hono } from "hono";
import { HTTPException } from "hono/http-exception";

export const exampleApi = new Hono()
  .post("/", sValidator("json", Example.CreateSchema), async (c) => {
    // create and return new record...
  })
  .get("/", async (c) => {
    // Get and return list...
  })
  .get("/:id", async (c) => {
    // Get and return one...
  })
  .put("/:id", sValidator("json", Example.UpdateSchema), async (c) => {
    // Update and return one...
  })
  .delete("/:id", async (c) => {
    // Delete and return one...
  });
```

### List Filtering

List endpoints (`GET /`) MAY accept optional query parameters for server-side filtering. When a module's core `list()` function supports filtering options, the route MUST use `sValidator("query", Module.ListQuerySchema)` and pass the query through:

```ts
.get("/", sValidator("query", Project.ListQuerySchema), async (c) => {
  const query = c.req.valid("query");
  const records = Project.list({ name: query.name });
  return c.json(records);
})
```

This keeps filtering efficient (server-side SQL `WHERE` clause) rather than fetching all records and filtering client-side. The `ListQuerySchema` MUST be defined in the core module's `schema.ts` with all fields optional, so calling the endpoint without query params returns all records.

- Validation MUST be done via the Hono `sValidator` using defined schemas from the respective core module.
- For single `get`, `put`, and `delete` routes - if records are not found, they MUST return a `404`.
  ```ts
  throw new HTTPException(404, { message: "record not found" });
  ```

### Test files

- All routes MUST have a colocated test file named `$MODULE_NAME.test.ts`.
- Tests MUST cover all CRUD routes.
- Tests use the [Bun test runner](https://bun.com/docs/test) (`bun:test`).
- Tests MUST call `runMigrations()` from `@bastion/core/drizzle` in a `beforeAll` hook to set up the schema.
- Tests SHOULD call `resetDatabase()` in a `beforeEach` hook to ensure isolation between tests.
- Tests MUST NOT define inline types. Use `v.parse()` with schemas from the respective core module to validate and type response bodies.

```ts
import { describe, test, expect, beforeAll, beforeEach } from "bun:test";
import { runMigrations, resetDatabase } from "@bastion/core/drizzle";
import { Example } from "@bastion/core/example";
import * as v from "valibot";
import { app } from "..";

beforeAll(() => {
  runMigrations();
});

beforeEach(() => {
  resetDatabase();
});

describe("POST /v1/example", () => {
  test("creates a record", async () => {
    const res = await app.request("/v1/example", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "test" }),
    });
    expect(res.status).toBe(200);

    const body = v.parse(Example.Schema, await res.json());
    expect(body.id).toBe(1);
    // Remaining assertions...
  });
});
```
