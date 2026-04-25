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
