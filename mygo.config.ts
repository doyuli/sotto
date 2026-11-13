import { defineConfig } from "mygo-cli";
import pkg from "./package.json" with { type: "json" };

const updatePublicKey = process.env.MYGO_UPDATER_PUBLIC_KEY?.trim();
const signingIdentity = process.env.MACOS_SIGNING_IDENTITY?.trim();
const notaryProfile = process.env.MACOS_NOTARY_PROFILE?.trim();

export default defineConfig({
  name: "Sotto",
  slug: "sotto",
  identifier: "app.sotto",
  version: pkg.version,
  updates: updatePublicKey ? { publicKey: updatePublicKey, github: "doyuli/sotto" } : undefined,
  out: "build",
  macos: {
    minimumSystemVersion: "14.0",
    signingIdentity: signingIdentity || undefined,
    notarize: notaryProfile ? {
      keychainProfile: notaryProfile,
      keychain: process.env.MACOS_NOTARY_KEYCHAIN?.trim() || undefined,
    } : undefined,
    infoPlist: {
      LSUIElement: true,
      NSScreenCaptureUsageDescription: "用于识别你主动框选的屏幕文字。",
    },
  },
});
