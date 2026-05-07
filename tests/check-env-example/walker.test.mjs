// Tests for the string- and comment-aware helpers in
// scripts/check-env-example.mjs. Covers the two edge cases called out in
// issue #821 (PR #818 review follow-up):
//
//   1. Paren-bearing string literals inside the const block must not
//      throw off the paren-depth walker.
//   2. Commented-out `<lowercase>Env = "CHAT_..."` bindings inside the
//      const block must not be picked up by the legacy regex.

import { describe, expect, it } from "vitest";

import {
  LEGACY_ENV_CONST_PATTERN,
  collectMatches,
  extractFirstConstBlock,
  stripGoComments,
} from "../../scripts/check-env-example.mjs";

describe("extractFirstConstBlock — string-aware paren tracking", () => {
  it("returns the body of a plain const block", () => {
    const src = `package main\n\nconst (\n\tportEnv = "CHAT_SERVER_PORT"\n\tdbPathEnv = "CHAT_DB_PATH"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`portEnv = "CHAT_SERVER_PORT"`);
    expect(body).toContain(`dbPathEnv = "CHAT_DB_PATH"`);
  });

  it("ignores `)` inside an interpreted string literal", () => {
    // The string contains an unbalanced `)` that, if counted, would close
    // the const block before `dbPathEnv` is seen.
    const src = `package main\n\nconst (\n\tportEnv = "CHAT_SERVER_PORT (legacy)"\n\tdbPathEnv = "CHAT_DB_PATH"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`portEnv = "CHAT_SERVER_PORT (legacy)"`);
    expect(body).toContain(`dbPathEnv = "CHAT_DB_PATH"`);
  });

  it("ignores `(` inside an interpreted string literal", () => {
    // A leading `(` would otherwise inflate depth and never reach 0.
    const src = `package main\n\nconst (\n\tportEnv = "CHAT_SERVER_PORT_("\n\tdbPathEnv = "CHAT_DB_PATH"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`portEnv`);
    expect(body).toContain(`dbPathEnv = "CHAT_DB_PATH"`);
    // The walker terminated; the body excludes the closing `)`.
    expect(body.endsWith("\n")).toBe(true);
  });

  it("ignores `)` inside a raw string literal", () => {
    const src =
      `package main\n\nconst (\n\tportEnv = ` +
      "`" +
      `CHAT_SERVER_PORT)` +
      "`" +
      `\n\tdbPathEnv = "CHAT_DB_PATH"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain("portEnv");
    expect(body).toContain(`dbPathEnv = "CHAT_DB_PATH"`);
  });

  it("ignores `)` inside a line comment", () => {
    const src = `package main\n\nconst (\n\tportEnv = "CHAT_SERVER_PORT" // closes the block? )\n\tdbPathEnv = "CHAT_DB_PATH"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`dbPathEnv = "CHAT_DB_PATH"`);
  });

  it("ignores `)` inside a block comment", () => {
    const src = `package main\n\nconst (\n\tportEnv = "CHAT_SERVER_PORT" /* not the closer ) */\n\tdbPathEnv = "CHAT_DB_PATH"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`dbPathEnv = "CHAT_DB_PATH"`);
  });

  it("returns empty string when no const block exists", () => {
    const src = `package main\n\nvar fooEnv = "CHAT_NOPE"\n`;
    expect(extractFirstConstBlock(src)).toBe("");
  });

  it("does not treat `// const (` inside a comment as the opener", () => {
    const src = `package main\n\n// const (\n//   ghostEnv = "CHAT_GHOST"\n// )\nconst (\n\tportEnv = "CHAT_SERVER_PORT"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`portEnv = "CHAT_SERVER_PORT"`);
    expect(body).not.toContain("ghostEnv");
  });

  it("does not treat `const Foo = 1` (single-binding) as a block", () => {
    const src = `package main\n\nconst Pi = 3.14\nconst (\n\tportEnv = "CHAT_SERVER_PORT"\n)\n`;
    const body = extractFirstConstBlock(src);
    expect(body).toContain(`portEnv = "CHAT_SERVER_PORT"`);
  });
});

describe("stripGoComments + legacy regex — commented-out bindings", () => {
  it('drops a line-commented `<lowercase>Env = "CHAT_..."` binding', () => {
    const block = `\n\tportEnv = "CHAT_SERVER_PORT"\n\t// ghostEnv = "CHAT_GHOST"\n`;
    const stripped = stripGoComments(block);
    const matches = collectMatches(stripped, LEGACY_ENV_CONST_PATTERN, "main.go");
    expect(matches.map((m) => m.name)).toEqual(["CHAT_SERVER_PORT"]);
  });

  it('drops a block-commented `<lowercase>Env = "CHAT_..."` binding', () => {
    const block = `\n\tportEnv = "CHAT_SERVER_PORT"\n\t/* ghostEnv = "CHAT_GHOST" */\n`;
    const stripped = stripGoComments(block);
    const matches = collectMatches(stripped, LEGACY_ENV_CONST_PATTERN, "main.go");
    expect(matches.map((m) => m.name)).toEqual(["CHAT_SERVER_PORT"]);
  });

  it("preserves a legitimate trailing comment without dropping the binding", () => {
    const block = `\n\tportEnv = "CHAT_SERVER_PORT" // listening port\n`;
    const stripped = stripGoComments(block);
    const matches = collectMatches(stripped, LEGACY_ENV_CONST_PATTERN, "main.go");
    expect(matches.map((m) => m.name)).toEqual(["CHAT_SERVER_PORT"]);
  });

  it("does not strip `//` that lives inside a string literal", () => {
    const block = `\n\turlEnv = "CHAT_FAKE_URL // not-a-comment"\n\tportEnv = "CHAT_SERVER_PORT"\n`;
    const stripped = stripGoComments(block);
    expect(stripped).toContain("CHAT_FAKE_URL // not-a-comment");
    const matches = collectMatches(stripped, LEGACY_ENV_CONST_PATTERN, "main.go");
    expect(matches.map((m) => m.name).sort()).toEqual([
      "CHAT_FAKE_URL // not-a-comment",
      "CHAT_SERVER_PORT",
    ]);
  });
});

describe("end-to-end edge cases on a synthetic main.go", () => {
  it("yields the un-commented bindings only", () => {
    const src = `package main\n\nconst (\n\tportEnv = "CHAT_SERVER_PORT"\n\t// ghostEnv = "CHAT_GHOST"\n\tdbPathEnv = "CHAT_DB_PATH (legacy))"\n\tinviteCodeEnv = "CHAT_INVITE_CODE"\n)\n\nvar leakEnv = "CHAT_LEAK"\n`;
    const body = extractFirstConstBlock(src);
    expect(body).not.toBe("");
    expect(body).not.toContain("leakEnv");
    const stripped = stripGoComments(body);
    const matches = collectMatches(stripped, LEGACY_ENV_CONST_PATTERN, "main.go");
    expect(matches.map((m) => m.name).sort()).toEqual([
      "CHAT_DB_PATH (legacy))",
      "CHAT_INVITE_CODE",
      "CHAT_SERVER_PORT",
    ]);
  });
});
