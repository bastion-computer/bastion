import * as v from "valibot";

export const ExampleSchema = v.object({
  id: v.number(),
  name: v.string(),
  createdAt: v.string(),
  updatedAt: v.string(),
});
export type ExampleSchema = v.InferInput<typeof ExampleSchema>;

export const CreateExampleSchema = v.object({
  name: v.string(),
});
export type CreateExampleSchema = v.InferInput<typeof CreateExampleSchema>;

export const UpdateExampleSchema = v.object({
  name: v.optional(v.string()),
});
export type UpdateExampleSchema = v.InferInput<typeof UpdateExampleSchema>;
