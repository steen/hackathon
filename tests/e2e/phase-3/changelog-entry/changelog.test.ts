// AC-1 (issue #661): `CHANGELOG.md` contains a `0.1.0` entry whose
// heading carries a release-day date.
//
// The repo and Keep-a-Changelog disagree on bracket style — accept
// `## [0.1.0] - YYYY-MM-DD` and unbracketed `## 0.1.0 - YYYY-MM-DD`.
// Don't pin the exact release date; the spec says "release day" and
// the test cannot know it ahead of time. A loose window
// (>= 2026-05-01, <= today + 1 day) catches the obvious failure
// modes (missing date, year typo, future-dated entry) without going
// red on a rebase that legitimately moves the release date.
//
// Fragment fallback: if a future flow aggregates per-PR fragments
// into the release entry, the file may live at
// `CHANGELOG.d/0.1.0.md`. Accept either source.

import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, it, expect } from "vitest";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..", "..");

const HEADING_RE = /^## \[?0\.1\.0\]?\s*[-—]\s*(\d{4})-(\d{2})-(\d{2})/m;

async function readIfExists(p: string): Promise<string | null> {
  try {
    return await readFile(p, "utf-8");
  } catch (e) {
    if (e instanceof Error && "code" in e && e.code === "ENOENT") return null;
    throw e;
  }
}

function parseUtcDate(y: string, m: string, d: string): Date | null {
  const date = new Date(Date.UTC(Number(y), Number(m) - 1, Number(d)));
  if (Number.isNaN(date.getTime())) return null;
  // Reject components like 2026-13-40 that Date silently rolls over.
  if (
    date.getUTCFullYear() !== Number(y) ||
    date.getUTCMonth() !== Number(m) - 1 ||
    date.getUTCDate() !== Number(d)
  ) {
    return null;
  }
  return date;
}

describe("changelog-entry AC-1: 0.1.0 entry exists with valid release-day date", () => {
  it("AC-1: a 0.1.0 heading with a valid YYYY-MM-DD date is present in CHANGELOG.md or CHANGELOG.d/0.1.0.md", async () => {
    const sources = [
      path.join(repoRoot, "CHANGELOG.md"),
      path.join(repoRoot, "CHANGELOG.d", "0.1.0.md"),
    ];

    let match: RegExpMatchArray | null = null;
    let matchedSource: string | null = null;
    for (const src of sources) {
      const body = await readIfExists(src);
      if (body === null) continue;
      const m = HEADING_RE.exec(body);
      if (m) {
        match = m;
        matchedSource = src;
        break;
      }
    }

    if (match === null || matchedSource === null) {
      expect.fail(
        `expected a 0.1.0 heading matching ${HEADING_RE.toString()} in one of: ${sources.join(", ")}`,
      );
    }

    const [, y, mo, d] = match;
    const parsed = parseUtcDate(y, mo, d);
    if (parsed === null) {
      expect.fail(`0.1.0 date '${y}-${mo}-${d}' is not a valid calendar date`);
    }

    const lower = Date.UTC(2026, 4, 1); // 2026-05-01
    const now = new Date();
    const upper =
      Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()) + 24 * 60 * 60 * 1000;

    const ts = parsed.getTime();
    expect(ts, `0.1.0 date ${y}-${mo}-${d} is before 2026-05-01`).toBeGreaterThanOrEqual(lower);
    expect(ts, `0.1.0 date ${y}-${mo}-${d} is more than one day in the future`).toBeLessThanOrEqual(
      upper,
    );
  });
});
