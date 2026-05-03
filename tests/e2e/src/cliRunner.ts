// Spawns the real `chatd` binary with isolated $XDG_CONFIG_HOME so each
// scenario gets its own token store. Returns stdout/stderr/exitCode so a
// failing scenario can show *which* command's output didn't match.

import { spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export interface CliResult {
  stdout: string;
  stderr: string;
  exitCode: number | null;
}

export interface CliSession {
  configHome: string;
  baseUrl: string;
  binary: string;
  run: (args: string[], opts?: RunOpts) => Promise<CliResult>;
  spawnLong: (args: string[], opts?: RunOpts) => SpawnedCli;
  cleanup: () => void;
}

export interface RunOpts {
  // Extra env merged on top of session defaults (e.g. CHAT_PASSWORD,
  // CHAT_INVITE_CODE, CHAT_USERNAME).
  env?: Record<string, string>;
  stdin?: string;
  timeoutMs?: number;
}

export interface SpawnedCli {
  child: ChildProcess;
  stdout: () => string;
  stderr: () => string;
  // Resolves when the child exits (clean or killed).
  exited: Promise<number | null>;
  stop: () => Promise<void>;
}

export function newCliSession(opts: { binary: string; baseUrl: string }): CliSession {
  const configHome = mkdtempSync(join(tmpdir(), "chatd-e2e-"));
  return {
    configHome,
    baseUrl: opts.baseUrl,
    binary: opts.binary,
    async run(args, runOpts) {
      return runOnce({ binary: opts.binary, baseUrl: opts.baseUrl, configHome, args, ...runOpts });
    },
    spawnLong(args, runOpts) {
      return spawnLong({
        binary: opts.binary,
        baseUrl: opts.baseUrl,
        configHome,
        args,
        env: runOpts?.env,
      });
    },
    cleanup() {
      try {
        rmSync(configHome, { recursive: true, force: true });
      } catch {
        // best-effort
      }
    },
  };
}

interface RunArgs extends RunOpts {
  binary: string;
  baseUrl: string;
  configHome: string;
  args: string[];
}

async function runOnce(opts: RunArgs): Promise<CliResult> {
  return new Promise((res, rej) => {
    const child = spawn(opts.binary, ["--server", opts.baseUrl, ...opts.args], {
      env: {
        ...process.env,
        XDG_CONFIG_HOME: opts.configHome,
        ...opts.env,
      },
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (b: Buffer) => {
      stdout += b.toString("utf8");
    });
    child.stderr.on("data", (b: Buffer) => {
      stderr += b.toString("utf8");
    });
    if (opts.stdin !== undefined) {
      child.stdin.write(opts.stdin);
      child.stdin.end();
    } else {
      child.stdin.end();
    }
    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      rej(new Error(`chatd ${opts.args.join(" ")} timed out\nstdout:${stdout}\nstderr:${stderr}`));
    }, opts.timeoutMs ?? 15_000);
    child.once("exit", (code) => {
      clearTimeout(timer);
      res({ stdout, stderr, exitCode: code });
    });
    child.once("error", rej);
  });
}

interface SpawnLongArgs {
  binary: string;
  baseUrl: string;
  configHome: string;
  args: string[];
  env?: Record<string, string>;
}

function spawnLong(opts: SpawnLongArgs): SpawnedCli {
  let stdout = "";
  let stderr = "";
  const child = spawn(opts.binary, ["--server", opts.baseUrl, ...opts.args], {
    env: {
      ...process.env,
      XDG_CONFIG_HOME: opts.configHome,
      ...opts.env,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.on("data", (b: Buffer) => {
    stdout += b.toString("utf8");
  });
  child.stderr.on("data", (b: Buffer) => {
    stderr += b.toString("utf8");
  });
  const exited = new Promise<number | null>((res) => {
    child.once("exit", (code) => {
      res(code);
    });
  });
  return {
    child,
    stdout: () => stdout,
    stderr: () => stderr,
    exited,
    async stop() {
      if (child.exitCode !== null) return;
      child.kill("SIGTERM");
      const winner = await Promise.race([
        exited,
        new Promise<"timeout">((res) => {
          setTimeout(() => {
            res("timeout");
          }, 3000);
        }),
      ]);
      if (winner === "timeout") {
        child.kill("SIGKILL");
        await exited;
      }
    },
  };
}
