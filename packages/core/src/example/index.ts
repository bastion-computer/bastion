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
