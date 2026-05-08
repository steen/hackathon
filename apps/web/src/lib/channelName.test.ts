import { describe, expect, it } from "vitest";
import { CHANNEL_NAME_RE, isValidChannelName } from "./channelName.js";

describe("isValidChannelName", () => {
  it("accepts lowercase letters, digits, hyphens with leading alnum", () => {
    expect(isValidChannelName("general")).toBe(true);
    expect(isValidChannelName("books")).toBe(true);
    expect(isValidChannelName("9-lives")).toBe(true);
    expect(isValidChannelName("a")).toBe(true);
    expect(isValidChannelName("a".repeat(40))).toBe(true);
  });

  it("rejects empty, too-long, uppercase, leading hyphen, and other characters", () => {
    expect(isValidChannelName("")).toBe(false);
    expect(isValidChannelName("a".repeat(41))).toBe(false);
    expect(isValidChannelName("Books")).toBe(false);
    expect(isValidChannelName("-leading")).toBe(false);
    expect(isValidChannelName("with space")).toBe(false);
    expect(isValidChannelName("with_underscore")).toBe(false);
    expect(isValidChannelName("emoji😀")).toBe(false);
  });

  it("regex shape matches the server contract", () => {
    expect(CHANNEL_NAME_RE.source).toBe("^[a-z0-9][a-z0-9-]{0,39}$");
  });
});
