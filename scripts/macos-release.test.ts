import { expect, test } from "bun:test";
import { mkdtempSync, readFileSync, writeFileSync, chmodSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, delimiter } from "node:path";

const workflow = Bun.YAML.parse(await Bun.file(new URL("../.github/workflows/release.yml", import.meta.url)).text()) as {
  jobs: { macos: { steps: { name?: string; run?: string; if?: string }[] } };
};
const build = workflow.jobs.macos.steps.find(step => step.name === "Build and upload Apple silicon Mac artifacts")?.run;
if (!build) throw new Error("macOS release build step is missing");

function runBuild(overrides: Record<string, string> = {}) {
  const dir = mkdtempSync(join(tmpdir(), "sotto-release-mode-"));
  try {
    const executable = join(dir, "bun");
    writeFileSync(executable, '#!/bin/bash\nprintf "%s\\n" "$@" > "$TEST_ARGS_FILE"\nprintf "%s" "${MACOS_NOTARY_PROFILE:-}" > "$TEST_PROFILE_FILE"\nexit "${TEST_BUILD_EXIT:-0}"\n');
    chmodSync(executable, 0o755);
    const result = Bun.spawnSync(["bash", "-e", "-o", "pipefail", "-c", build!], {
      env: {
        ...process.env,
        PATH: `${dir}${delimiter}${process.env.PATH}`,
        MYGO_UPDATER_PUBLIC_KEY: "test-public-key",
        MYGO_UPDATER_PRIVATE_KEY: "test-private-key",
        MACOS_DEVELOPER_ID_RELEASE: "",
        MACOS_NOTARY_PROFILE: "",
        MACOS_SIGNING_IDENTITY: "",
        TEST_ARGS_FILE: join(dir, "args"),
        TEST_PROFILE_FILE: join(dir, "profile"),
        TEST_BUILD_EXIT: "0",
        ...overrides,
      },
      stdout: "pipe", stderr: "pipe",
    });
    return {
      code: result.exitCode,
      args: existsSync(join(dir, "args")) ? readFileSync(join(dir, "args"), "utf8").trim().split("\n") : null,
      profile: existsSync(join(dir, "profile")) ? readFileSync(join(dir, "profile"), "utf8") : null,
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test("release without Apple credentials still builds and uploads all Mac artifacts", () => {
  // The former unconditional credential step prevented no-account releases.
  expect(runBuild()).toEqual({
    code: 0, args: ["run", "build:mac", "-sign", "-", "-skip-notarize", "-upload"], profile: "",
  });
});

test("disabled Developer ID mode overrides leftover signing configuration", () => {
  const result = runBuild({ MACOS_DEVELOPER_ID_RELEASE: "false", MACOS_SIGNING_IDENTITY: "old identity", MACOS_NOTARY_PROFILE: "old-profile" });
  expect(result.code).toBe(0);
  expect(result.args).toEqual(["run", "build:mac", "-sign", "-", "-skip-notarize", "-upload"]);
});

test("explicit Developer ID mode uses the prepared notarization profile", () => {
  expect(runBuild({ MACOS_DEVELOPER_ID_RELEASE: "true", MACOS_SIGNING_IDENTITY: "Developer ID Application: Example" })).toEqual({
    code: 0, args: ["run", "build:mac", "-upload"], profile: "sotto-ci",
  });
});

for (const key of ["MYGO_UPDATER_PUBLIC_KEY", "MYGO_UPDATER_PRIVATE_KEY"]) {
  test(`missing ${key} prevents upload even without Apple signing`, () => {
    const result = runBuild({ [key]: "" });
    expect(result.code).not.toBe(0);
    expect(result.args).toBeNull();
  });
}

test("a mistyped release mode fails instead of silently changing signing policy", () => {
  const result = runBuild({ MACOS_DEVELOPER_ID_RELEASE: "treu" });
  expect(result.code).not.toBe(0);
  expect(result.args).toBeNull();
});

test("packaging or upload errors fail the job", () => {
  expect(runBuild({ TEST_BUILD_EXIT: "17" }).code).toBe(17);
});


test("Apple credential import and cleanup require explicit opt-in", () => {
  // Building in ad-hoc mode is insufficient if an earlier step still requires paid credentials.
  const steps = workflow.jobs.macos.steps;
  expect(steps.find(step => step.name === "Prepare Developer ID and notarization credentials")?.if)
    .toBe("vars.MACOS_DEVELOPER_ID_RELEASE == 'true'");
  expect(steps.find(step => step.name === "Remove signing credentials")?.if)
    .toBe("always() && vars.MACOS_DEVELOPER_ID_RELEASE == 'true'");
});
