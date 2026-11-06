import { defineConfig } from "mygo-cli";
import pkg from "./package.json" with { type: "json" };

const updatePublicKey = process.env.MYGO_UPDATER_PUBLIC_KEY?.trim();

export default defineConfig({
  name: "Sotto",
  slug: "sotto",
  identifier: "app.sotto",
  version: pkg.version,
  updates: updatePublicKey ? { publicKey: updatePublicKey, github: "doyuli/sotto" } : undefined,
  // `mygo dev` runs devCommand and loads devUrl; `mygo build` runs
  // buildCommand and embeds frontendDist into the app.
  devUrl: "http://localhost:5173",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
});
