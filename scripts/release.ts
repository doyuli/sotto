import { open, readFile, unlink, writeFile } from "node:fs/promises";
import { clearScreenDown, emitKeypressEvents, moveCursor, type Key } from "node:readline";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

const cwd = fileURLToPath(new URL("../", import.meta.url));
const packagePath = new URL("../package.json", import.meta.url);
const usage = "bun run release [patch|minor|major|X.Y.Z] [--dry-run]";

function git(...args: string[]): string {
  const result = Bun.spawnSync(["git", ...args], { cwd, stdout: "pipe", stderr: "inherit" });
  const output = result.stdout.toString().trim();
  if (result.exitCode !== 0) {
    if (output) console.error(output);
    throw new Error(`git ${args.join(" ")} 失败`);
  }
  return output;
}

function versionParts(version: string) {
  const [major, minor, patch] = version.split(".");
  const parts = [Number(major), Number(minor), Number(patch)] as const;
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version) ||
      parts.some((part) => part > 65535)) {
    throw new Error("版本号须为 X.Y.Z，每段为 0 到 65535 的整数");
  }
  return parts;
}

function chooseBump(version: string, bumps: ReadonlyMap<string, string>): Promise<string | undefined> {
  const { stdin, stdout } = process;
  if (!stdin.isTTY || !stdout.isTTY) throw new Error(usage);

  const options = [...bumps];
  let selected = 0;
  const render = () => {
    stdout.write(options.map(([name, next], i) =>
      `  ${i === selected ? "❯" : " "} ${name.padEnd(6)} ${next}\n`).join(""));
  };

  const wasRaw = stdin.isRaw;
  emitKeypressEvents(stdin);
  stdin.setRawMode(true);
  return new Promise((resolve) => {
    function finish(choice?: string) {
      stdin.off("keypress", onKey);
      stdin.off("end", onEnd);
      stdin.setRawMode(wasRaw);
      stdin.pause();
      resolve(choice);
    }

    function onEnd() {
      finish();
    }

    function onKey(_text: string, key: Key) {
      if (key.name === "escape" || (key.ctrl && key.name === "c")) {
        finish();
      } else if (key.name === "return" || key.name === "enter") {
        finish(options[selected]?.[0]);
      } else if (key.name === "up" || key.name === "down") {
        const step = key.name === "up" ? -1 : 1;
        selected = (selected + step + options.length) % options.length;
        moveCursor(stdout, 0, -options.length);
        clearScreenDown(stdout);
        render();
      }
    }

    stdin.on("keypress", onKey);
    stdin.once("end", onEnd);
    console.log(`当前为 ${version}。用 ↑/↓ 选择，Enter 确认，Esc 取消。`);
    render();
    stdin.resume();
  });
}

async function release() {
  const { values, positionals } = parseArgs({
    allowPositionals: true,
    options: { "dry-run": { type: "boolean" }, help: { type: "boolean", short: "h" } },
  });
  if (values.help) {
    console.log(usage);
    return;
  }
  if (positionals.length > 1) throw new Error(usage);

  const source = await readFile(packagePath, "utf8");
  const pkg = JSON.parse(source);
  const current = versionParts(pkg.version);
  const [major, minor, patch] = current;
  const bumps = new Map([
    ["patch", `${major}.${minor}.${patch + 1}`],
    ["minor", `${major}.${minor + 1}.0`],
    ["major", `${major + 1}.0.0`],
  ]);
  const choice = positionals[0] ?? await chooseBump(pkg.version, bumps);
  if (!choice) {
    console.log("已取消发版。");
    return;
  }

  const version = bumps.get(choice) ?? choice;
  const [nextMajor, nextMinor, nextPatch] = versionParts(version);
  const difference = (nextMajor - major) || (nextMinor - minor) || (nextPatch - patch);
  if (difference <= 0) {
    throw new Error(`新版本必须高于 ${pkg.version}`);
  }
  const tag = `v${version}`;
  console.log(`${pkg.version} → ${version} (${tag})`);
  if (values["dry-run"]) return;

  const lockPath = git("rev-parse", "--path-format=absolute", "--git-path", "sotto-release.lock");
  const lock = await open(lockPath, "wx");
  try {
    if (git("branch", "--show-current") !== "main" || git("status", "--porcelain")) {
      throw new Error("请在工作区干净的 main 分支发版");
    }
    git("fetch", "--no-tags", "origin", "refs/heads/main");
    if (git("rev-parse", "HEAD") !== git("rev-parse", "FETCH_HEAD")) {
      throw new Error("请先将本地 main 与 origin/main 同步");
    }
    const tagRef = `refs/tags/${tag}`;
    if (git("tag", "--list", tag) || git("ls-remote", "--refs", "origin", tagRef)) {
      throw new Error(`标签 ${tag} 已存在`);
    }
    if (await readFile(packagePath, "utf8") !== source) {
      throw new Error("package.json 已变化，请重新运行发版命令");
    }

    pkg.version = version;
    await writeFile(packagePath, JSON.stringify(pkg, null, 2) + "\n");
    git("commit", "--only", "-m", `chore(release): ${tag}`, "--", "package.json");
    git("tag", "-a", tag, "-m", tag);
    git("push", "--atomic", "origin", "refs/heads/main", tagRef);
  } finally {
    await lock.close();
    await unlink(lockPath);
  }
  console.log(`已推送 ${tag}，GitHub Actions 将构建草稿 Release。`);
}

try {
  await release();
} catch (error) {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
}
