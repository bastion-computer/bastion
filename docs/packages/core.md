This package contains business logic modules that are reusable across all other packages

## Modules

This section provides a guideline for creating new module for a specific domain.

### Module files

- All modules MUST be created with the following files under `src/$MODULE_NAME` directory:
  - `schema.ts`: Contains Valibot schemas and inferred types.
  - `index.ts`: Contains the main business logic for the model.
  - `$MODULE_NAME.sql.ts`: Containing the Drizzle ORM instantiation. Skip this if the module does not require a DB table.
  - `$MODULE_NAME.test.ts`: Contains tests for the module using the Bun test runner.

### Module types

- All types MUST be Valibot schemas defined in `schema.ts` with types inferred via `v.InferInput`.
- NEVER define inline types in `index.ts`, `$MODULE_NAME.sql.ts`, or any other file.
- Every type starts as a Valibot schema constant, with the TypeScript type inferred from it:

  ```ts
  export const Schema = v.object({ ... });
  export type Schema = v.InferInput<typeof Schema>;
  ```

- This applies to all data shapes: input schemas, query schemas, result types, union types, record types, etc.
- Types used in `$MODULE_NAME.sql.ts` via `$type<T>()` MUST be imported from `schema.ts`.

**Centralizing types here allows consistently strong types and validation across the codebase**.

### Module namespace

- The `index.ts` files MUST export a namespace of format `ModuleName`.
- The namespace MUST re-export schemas and types from `schema.ts`. NEVER redefine or inline them.
- Modules with a database table SHOULD implement CRUD functions (`create`, `get`, `list`, `update`, `remove`).

```ts
import { eq } from "drizzle-orm";
import { db } from "../drizzle";
import { exampleTable } from "./example.sql";
import * as ExampleSchema from "./schema";

export namespace Example {
  export const Schema = ExampleSchema.ExampleSchema;
  export type Schema = ExampleSchema.ExampleSchema;

  export const CreateSchema = ExampleSchema.CreateExampleSchema;
  export type CreateSchema = ExampleSchema.CreateExampleSchema;

  export const UpdateSchema = ExampleSchema.UpdateExampleSchema;
  export type UpdateSchema = ExampleSchema.UpdateExampleSchema;

  export function create(input: CreateSchema) {
    const now = new Date().toISOString();
    return db
      .insert(exampleTable)
      .values({
        name: input.name,
        createdAt: now,
        updatedAt: now,
      })
      .returning()
      .get();
  }

  export function get(id: number) {
    return db.select().from(exampleTable).where(eq(exampleTable.id, id)).get();
  }

  export function list() {
    return db.select().from(exampleTable).all();
  }

  export function update(id: number, input: UpdateSchema) {
    return db
      .update(exampleTable)
      .set({
        ...input,
        updatedAt: new Date().toISOString(),
      })
      .where(eq(exampleTable.id, id))
      .returning()
      .get();
  }

  export function remove(id: number) {
    return db
      .delete(exampleTable)
      .where(eq(exampleTable.id, id))
      .returning()
      .get();
  }
}
```

### Module tests

- All modules MUST have a colocated test file named `$MODULE_NAME.test.ts`.
- Tests use the [Bun test runner](https://bun.com/docs/test) (`bun:test`).
- Tests MUST call `initDb(":memory:")` from `../drizzle` before `runMigrations()` to use an **in-memory SQLite** database. No mocking is required -- module code imports `db` from `../drizzle` as normal.
- Tests MUST call `runMigrations()` from `../drizzle` in a `beforeAll` hook (after `initDb`) to set up the schema.
- Tests MUST call `resetDatabase()` from `../drizzle` in a `beforeEach` hook to ensure isolation between tests.

```ts
import { describe, test, expect, beforeAll, beforeEach } from "bun:test";
import { initDb, runMigrations, resetDatabase } from "../drizzle";
import { Example } from ".";

beforeAll(() => {
  initDb(":memory:");
  runMigrations();
});

beforeEach(() => {
  resetDatabase();
});

describe("Example", () => {
  test("creates a record", () => {
    const result = Example.create({ name: "test" });
    expect(result.id).toBe(1);
    expect(result.name).toBe("test");
  });
});
```

### Drizzle exception

The only exception to this module pattern is `src/drizzle` which has an `index.ts` that initializes the database. It exports an `initDb(dataDir: string)` function that callers invoke before using `db`, `runMigrations()`, or `resetDatabase()`.

- `initDb(dataDir: string)` - Creates the data directory if it doesn't exist (unless `dataDir` is `":memory:"`), opens a SQLite database at `dataDir/sqlite.db`, enables `PRAGMA foreign_keys`, and sets the module-level `db` and `sqlite` variables.
- `runMigrations()` - Applies the migrations from the `migrations` directory to the database. Must be called after `initDb`.
- `resetDatabase()` - Deletes all rows from every user-defined table and resets autoincrement counters. Used in `beforeEach` hooks to ensure test isolation. Must be called after `initDb`.

## Database

An embedded Bun:SQLite database is managed by Drizzle ORM. All database commands can be handled via the `bun run db` script which is a proxy for running `drizzle-kit`.

### Migrations

- The `migrations` directory is controlled by Drizzle ORM and SHOULD NOT be manually edited.

### Generating migrations

1. Run `bun run db generate` in the `core` directory.
   - This command will create subsequent SQL and journal entries in the `migrations` directory.
   - It will not make changes to the database yet.
2. ALWAYS ask me to verify the changes before continuing.
3. `bun run db migrate` in the `core` directory.
   - This command will execute the migration to change the database schema.
