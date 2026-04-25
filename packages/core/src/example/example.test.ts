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
  describe("create", () => {
    test("inserts a record and returns it with all fields", () => {
      const result = Example.create({ name: "test" });

      expect(result.id).toBe(1);
      expect(result.name).toBe("test");
      expect(result.createdAt).toBeString();
      expect(result.updatedAt).toBeString();
      expect(result.createdAt).toBe(result.updatedAt);
    });
  });

  describe("get", () => {
    test("retrieves a record by id", () => {
      const created = Example.create({ name: "findme" });
      const result = Example.get(created.id);

      expect(result).toBeDefined();
      expect(result!.id).toBe(created.id);
      expect(result!.name).toBe("findme");
    });

    test("returns undefined for non-existent id", () => {
      const result = Example.get(999);

      expect(result).toBeUndefined();
    });
  });

  describe("list", () => {
    test("returns all records", () => {
      Example.create({ name: "first" });
      Example.create({ name: "second" });
      Example.create({ name: "third" });

      const result = Example.list();

      expect(result).toHaveLength(3);
      expect(result.map((r) => r.name)).toEqual(["first", "second", "third"]);
    });

    test("returns empty array when no records exist", () => {
      const result = Example.list();

      expect(result).toEqual([]);
    });
  });

  describe("update", () => {
    test("modifies a record and updates the updatedAt timestamp", async () => {
      const created = Example.create({ name: "original" });

      // Small delay to ensure updatedAt differs
      await new Promise((resolve) => setTimeout(resolve, 10));

      const updated = Example.update(created.id, { name: "modified" });

      expect(updated).toBeDefined();
      expect(updated!.id).toBe(created.id);
      expect(updated!.name).toBe("modified");
      expect(updated!.createdAt).toBe(created.createdAt);
      expect(updated!.updatedAt).not.toBe(created.updatedAt);
    });
  });

  describe("remove", () => {
    test("deletes a record and returns it", () => {
      const created = Example.create({ name: "deleteme" });
      const removed = Example.remove(created.id);

      expect(removed).toBeDefined();
      expect(removed!.id).toBe(created.id);
      expect(removed!.name).toBe("deleteme");

      const result = Example.get(created.id);
      expect(result).toBeUndefined();
    });

    test("returns undefined for non-existent id", () => {
      const result = Example.remove(999);

      expect(result).toBeUndefined();
    });
  });
});
