import { int, sqliteTable, text } from "drizzle-orm/sqlite-core";

export const exampleTable = sqliteTable("example", {
  id: int().primaryKey({ autoIncrement: true }),
  name: text().notNull(),
  createdAt: text().notNull(),
  updatedAt: text().notNull(),
});
