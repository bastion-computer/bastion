import { initDb, runMigrations } from "@bastion/core/drizzle";
import { Command } from "commander";
import { Hono } from "hono";
import { HTTPException } from "hono/http-exception";
import { mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { exampleApi } from "./example";

let port = 3148;

const isMain = Bun.main === import.meta.path;
if (isMain) {
  const program = new Command();
  program
    .option(
      "--port <number>",
      "port to listen on",
      (v) => parseInt(v, 10),
      3148,
    )
    .option("--data-dir <path>", "directory for persistent data", "~/.bastion");
  program.parse();

  const opts = program.opts();
  port = opts.port;
  const dataDir = resolve(opts.dataDir.replace(/^~/, homedir()));

  mkdirSync(dataDir, { recursive: true });
  initDb(dataDir);
  runMigrations();
}

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
export default { port, fetch: app.fetch };
