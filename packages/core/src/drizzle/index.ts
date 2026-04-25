import { Database } from "bun:sqlite";
import { drizzle } from "drizzle-orm/bun-sqlite";
import { migrate } from "drizzle-orm/bun-sqlite/migrator";

const isTest = process.env.NODE_ENV === "test";
const sqlite = new Database(isTest ? ":memory:" : "bastion.db");
// SQLite disables foreign key enforcement by default. Without this,
// ON DELETE CASCADE constraints are silently ignored.
sqlite.run("PRAGMA foreign_keys = ON");

export const db = drizzle(sqlite);

export function runMigrations() {
  const migrationsFolder = `${import.meta.dir}/../../migrations`;
  migrate(db, { migrationsFolder });
}

export function resetDatabase() {
  // Query all user-defined tables from SQLite's schema table, excluding
  // internal SQLite tables (sqlite_*) and Drizzle migration tables (__drizzle*).
  const tables = sqlite
    .query(
      `SELECT name FROM sqlite_master
       WHERE type='table'
         AND name NOT LIKE 'sqlite_%'
         AND name NOT LIKE '__drizzle%'`,
    )
    .all() as { name: string }[];

  // Delete all rows from each user table.
  for (const { name } of tables) {
    sqlite.run(`DELETE FROM "${name}"`);
  }

  // Reset autoincrement counters so IDs start fresh from 1.
  // sqlite_sequence tracks the last assigned rowid per table when AUTOINCREMENT is used.
  sqlite.run("DELETE FROM sqlite_sequence");
}
