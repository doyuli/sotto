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
  resources: ["LICENSE"],
  // Tray images are embedded in the executable.
  resourcesExclude: ["tray"],
  macos: {
    minimumSystemVersion: "14.0",
    infoPlist: {
      LSUIElement: true,
      NSScreenCaptureUsageDescription: "用于识别你主动框选的屏幕文字。",
    },
  },
});
