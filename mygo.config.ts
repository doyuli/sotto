import { defineConfig } from "mygo-cli";
import pkg from "./package.json" with { type: "json" };

const updatePublicKey = process.env.MYGO_UPDATER_PUBLIC_KEY?.trim();

export default defineConfig({
  name: "Sotto",
  slug: "sotto",
  identifier: "app.sotto",
  version: pkg.version,
  updates: updatePublicKey ? { publicKey: updatePublicKey, github: "doyuli/sotto" } : undefined,
  out: "build",
});
