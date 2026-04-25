import { defineConfig } from "drizzle-kit";

export default defineConfig({
  dialect: "sqlite",
  schema: "./src/**/*.sql.ts",
  out: "./migrations",
  dbCredentials: {
    url: "bastion.db",
  },
});
