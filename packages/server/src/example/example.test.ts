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

async function createExample(name: string) {
  const res = await app.request("/v1/example", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  return res;
}

describe("POST /v1/example", () => {
  test("creates a record", async () => {
    const res = await createExample("test");
    expect(res.status).toBe(200);

    const body = v.parse(Example.Schema, await res.json());
    expect(body.id).toBe(1);
    expect(body.name).toBe("test");
    expect(body.createdAt).toBeString();
    expect(body.updatedAt).toBeString();
  });
});

describe("GET /v1/example", () => {
  test("returns all records", async () => {
    await createExample("first");
    await createExample("second");

    const res = await app.request("/v1/example");
    expect(res.status).toBe(200);

    const body = v.parse(v.array(Example.Schema), await res.json());
    expect(body).toHaveLength(2);
    expect(body[0]!.name).toBe("first");
    expect(body[1]!.name).toBe("second");
  });

  test("returns empty array when no records exist", async () => {
    const res = await app.request("/v1/example");
    expect(res.status).toBe(200);

    const body = v.parse(v.array(Example.Schema), await res.json());
    expect(body).toEqual([]);
  });
});

describe("GET /v1/example/:id", () => {
  test("returns a record by id", async () => {
    const createRes = await createExample("findme");
    const created = v.parse(Example.Schema, await createRes.json());

    const res = await app.request(`/v1/example/${created.id}`);
    expect(res.status).toBe(200);

    const body = v.parse(Example.Schema, await res.json());
    expect(body.id).toBe(created.id);
    expect(body.name).toBe("findme");
  });

  test("returns 404 for non-existent id", async () => {
    const res = await app.request("/v1/example/999");
    expect(res.status).toBe(404);
  });
});

describe("PUT /v1/example/:id", () => {
  test("updates a record", async () => {
    const createRes = await createExample("original");
    const created = v.parse(Example.Schema, await createRes.json());

    const res = await app.request(`/v1/example/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "modified" }),
    });
    expect(res.status).toBe(200);

    const body = v.parse(Example.Schema, await res.json());
    expect(body.id).toBe(created.id);
    expect(body.name).toBe("modified");
  });

  test("returns 404 for non-existent id", async () => {
    const res = await app.request("/v1/example/999", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: "nope" }),
    });
    expect(res.status).toBe(404);
  });
});

describe("DELETE /v1/example/:id", () => {
  test("deletes a record", async () => {
    const createRes = await createExample("deleteme");
    const created = v.parse(Example.Schema, await createRes.json());

    const res = await app.request(`/v1/example/${created.id}`, {
      method: "DELETE",
    });
    expect(res.status).toBe(200);

    const body = v.parse(Example.Schema, await res.json());
    expect(body.id).toBe(created.id);
    expect(body.name).toBe("deleteme");

    // Verify it's gone
    const getRes = await app.request(`/v1/example/${created.id}`);
    expect(getRes.status).toBe(404);
  });

  test("returns 404 for non-existent id", async () => {
    const res = await app.request("/v1/example/999", {
      method: "DELETE",
    });
    expect(res.status).toBe(404);
  });
});
