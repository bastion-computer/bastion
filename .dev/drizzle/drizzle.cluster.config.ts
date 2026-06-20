import { defineConfig } from "drizzle-kit";

export default defineConfig({
  dialect: "postgresql",
  dbCredentials: {
    url:
      process.env.BASTION_CLUSTER_DATABASE_URL ??
      "postgres://bastion:bastion@localhost:3152/bastion_cluster?sslmode=disable",
  },
});
