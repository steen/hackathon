// Phase-10 identity-derivation cross-language byte-equivalence test.
// Decision-log §4 + L3 + L37 (`lt -p e2e-encryption 3`).
//
// The fixture (passphrase + username) is fixed; the expected base64-
// encoded pubkeys were computed once via packages/go-client's
// DeriveIdentity and committed here. Both clients MUST derive the
// SAME bytes — any drift (Argon2id parameter change, HKDF info-string
// edit, BoxSeedKeypair mismatch, salt-truncation slip) fails this test
// before it can ship.
//
// The Go-side counterpart in identity_vectors_test.go reads the same
// fixture and asserts the same expected bytes against the goclient
// derivation; running `pnpm vitest run` here covers the TS path,
// `go test ./tests/e2e/phase-10` covers the Go path.

import { describe, it, expect, beforeAll } from "vitest";
import { b64, deriveIdentity, ready } from "@hackathon/api-client";

// Fixture from the AC: passphrase = "correct horse battery staple",
// username = "alice". DO NOT change without updating the Go-side test
// (tests/e2e/phase-10/identity_vectors_test.go) and the expected
// constants below in lockstep.
const FIXTURE_PASSPHRASE = "correct horse battery staple";
const FIXTURE_USERNAME = "alice";

// Expected pubkeys (base64 of raw 32 bytes each), pinned via the
// committed Go-side derivation. If a future change is intentional, run
// the Go derivation locally to regenerate these constants and update
// both sides in the same PR.
const EXPECTED_BOX_PUBKEY = "FqIApyhlalBwzT8Ms8+ioqp3oRXwoTsP/hI8TjYUgl8=";
const EXPECTED_SIGN_PUBKEY = "Oo4hSOrlTPd4CdAShTbKRpYzucsSWuLW0734a0GJx/U=";

beforeAll(async () => {
  await ready();
});

describe("Phase-10 identity derivation — cross-language byte-equivalence", () => {
  it("TS-side derivation matches the pinned (Go-derived) box_pubkey + sign_pubkey", async () => {
    const id = await deriveIdentity(FIXTURE_PASSPHRASE, FIXTURE_USERNAME);
    expect(b64(id.boxPub)).toBe(EXPECTED_BOX_PUBKEY);
    expect(b64(id.signPub)).toBe(EXPECTED_SIGN_PUBKEY);
  }, 30_000);

  it("derivation is deterministic — same input produces the same bytes", async () => {
    const a = await deriveIdentity(FIXTURE_PASSPHRASE, FIXTURE_USERNAME);
    const b = await deriveIdentity(FIXTURE_PASSPHRASE, FIXTURE_USERNAME);
    expect(b64(a.boxPub)).toBe(b64(b.boxPub));
    expect(b64(a.signPub)).toBe(b64(b.signPub));
  }, 30_000);

  it("a one-byte passphrase change yields a fully different identity", async () => {
    const original = await deriveIdentity(FIXTURE_PASSPHRASE, FIXTURE_USERNAME);
    const tweaked = await deriveIdentity(FIXTURE_PASSPHRASE + "!", FIXTURE_USERNAME);
    expect(b64(tweaked.boxPub)).not.toBe(b64(original.boxPub));
    expect(b64(tweaked.signPub)).not.toBe(b64(original.signPub));
  }, 30_000);

  it("a username change yields a fully different identity (same passphrase)", async () => {
    const original = await deriveIdentity(FIXTURE_PASSPHRASE, FIXTURE_USERNAME);
    const otherUser = await deriveIdentity(FIXTURE_PASSPHRASE, "bob");
    expect(b64(otherUser.boxPub)).not.toBe(b64(original.boxPub));
    expect(b64(otherUser.signPub)).not.toBe(b64(original.signPub));
  }, 30_000);
});
