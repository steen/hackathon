import { execFileSync, spawnSync, type SpawnSyncReturns } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

export const repoRoot = resolve(__dirname, "..", "..", "..");

export function makeScaffoldTmpdir(): string {
  const dir = mkdtempSync(join(tmpdir(), "scaffold-e2e-"));

  for (const file of ["package.json", "pnpm-workspace.yaml", ".gitignore"]) {
    cpSync(join(repoRoot, file), join(dir, file));
  }

  mkdirSync(join(dir, "packages", "scaffold-stub"), { recursive: true });
  cpSync(
    join(repoRoot, "packages", "scaffold-stub", "package.json"),
    join(dir, "packages", "scaffold-stub", "package.json"),
  );

  return dir;
}

export function cleanup(dir: string): void {
  if (dir.startsWith(tmpdir())) {
    rmSync(dir, { recursive: true, force: true });
  }
}

export function pnpmInstallSilent(cwd: string): SpawnSyncReturns<string> {
  return spawnSync(
    "pnpm",
    ["install", "--prefer-offline", "--reporter=silent"],
    { cwd, encoding: "utf8", env: cleanEnv() },
  );
}

export function runPnpm(
  args: string[],
  cwd: string,
  opts: { timeoutMs?: number } = {},
): SpawnSyncReturns<string> {
  return spawnSync("pnpm", args, {
    cwd,
    encoding: "utf8",
    timeout: opts.timeoutMs,
    env: cleanEnv(),
  });
}

function cleanEnv(): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env };
  delete env.npm_config_user_agent;
  delete env.npm_lifecycle_event;
  delete env.npm_lifecycle_script;
  delete env.npm_package_json;
  delete env.npm_package_name;
  delete env.npm_package_version;
  delete env.npm_execpath;
  delete env.npm_node_execpath;
  delete env.PNPM_SCRIPT_SRC_DIR;
  return env;
}
