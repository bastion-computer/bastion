import { Example } from "@bastion/core/example";
import { sValidator } from "@hono/standard-validator";
import { Hono } from "hono";
import { HTTPException } from "hono/http-exception";

export const exampleApi = new Hono()
  .post("/", sValidator("json", Example.CreateSchema), async (c) => {
    const body = c.req.valid("json");
    const record = Example.create(body);
    return c.json(record);
  })
  .get("/", async (c) => {
    const records = Example.list();
    return c.json(records);
  })
  .get("/:id", async (c) => {
    const id = Number(c.req.param("id"));
    const record = Example.get(id);
    if (!record) {
      throw new HTTPException(404, { message: "record not found" });
    }
    return c.json(record);
  })
  .put("/:id", sValidator("json", Example.UpdateSchema), async (c) => {
    const id = Number(c.req.param("id"));
    const body = c.req.valid("json");
    const record = Example.update(id, body);
    if (!record) {
      throw new HTTPException(404, { message: "record not found" });
    }
    return c.json(record);
  })
  .delete("/:id", async (c) => {
    const id = Number(c.req.param("id"));
    const record = Example.remove(id);
    if (!record) {
      throw new HTTPException(404, { message: "record not found" });
    }
    return c.json(record);
  });
