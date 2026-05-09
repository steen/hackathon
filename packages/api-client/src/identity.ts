// Phase-10 identity derivation: passphrase + username → (box, sign)
// keypairs. Mirrors apps/server/internal/auth/identity.go byte-for-byte.
// Decision-log §4 + L3 + L37.
//
// libsodium-wrappers-sumo is required (NOT bare libsodium-wrappers):
// the standard build does not ship `crypto_pwhash` (Argon2id). Every
// entry-point that exercises crypto MUST `await ready()` before any
// identity API call (decision-log L27). The CLI-side equivalent is in
// apps/server/internal/auth/identity.go.

import sodium from "libsodium-wrappers-sumo";

import {
  ARGON_KEY_LEN,
  ARGON_MEMORY_BYTES,
  ARGON_TIME,
  HKDF_INFO_BOX,
  HKDF_INFO_SIGN,
  MIN_IDENTITY_PASSPHRASE_LEN,
  SALT_LEN,
  SALT_PREFIX,
} from "./identity_params.js";

export interface DerivedIdentity {
  rootSeed: Uint8Array;
  boxSeed: Uint8Array;
  signSeed: Uint8Array;
  boxPub: Uint8Array; // 32 bytes
  boxPriv: Uint8Array; // 32 bytes
  signPub: Uint8Array; // 32 bytes
  signPriv: Uint8Array; // 64 bytes (libsodium concat of seed||pubkey)
}

// ready awaits libsodium-wrappers-sumo's async init. Safe to call any
// number of times; resolves on the same internal promise. The web entry
// (apps/web/src/main.tsx) and any test that exercises crypto must await
// this before invoking any other identity helper or sodium API.
export async function ready(): Promise<void> {
  await sodium.ready;
}

// identitySalt computes SHA-256(SALT_PREFIX + username) truncated to
// SALT_LEN. The username is taken byte-for-byte (already lowercase-ASCII
// per the L37 registration regex); implementations MUST NOT apply
// `toLowerCase()` here — that would silently mask any L37 regression.
export async function identitySalt(username: string): Promise<Uint8Array> {
  const enc = new TextEncoder();
  const inputBuf = copyToArrayBuffer(enc.encode(SALT_PREFIX + username));
  const buf = await crypto.subtle.digest("SHA-256", inputBuf);
  return new Uint8Array(buf).slice(0, SALT_LEN);
}

// hkdfDeriveBits runs HKDF-SHA256 over (root, info) with an empty salt
// and returns the requested number of bytes. Matches the Go-side
// hkdf.New(sha256.New, root, nil, []byte(info)) byte-for-byte.
async function hkdfDeriveBits(
  root: Uint8Array,
  info: Uint8Array,
  bits: number,
): Promise<Uint8Array> {
  // SubtleCrypto's lib.dom typings expect Uint8Array<ArrayBuffer> for
  // BufferSource. libsodium's Uint8Array<ArrayBufferLike> is wider; copy
  // into a fresh ArrayBuffer-backed view so the type narrows. Same for
  // the info bytes from TextEncoder under strict-type-check.
  const rootBuf = copyToArrayBuffer(root);
  const infoBuf = copyToArrayBuffer(info);
  const saltBuf = new ArrayBuffer(0);
  const key = await crypto.subtle.importKey("raw", rootBuf, { name: "HKDF" }, false, [
    "deriveBits",
  ]);
  const out = await crypto.subtle.deriveBits(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: saltBuf,
      info: infoBuf,
    },
    key,
    bits,
  );
  return new Uint8Array(out);
}

// copyToArrayBuffer returns a Uint8Array backed by a freshly-allocated
// ArrayBuffer (NOT SharedArrayBuffer). lib.dom.d.ts under strict-type-
// check narrows BufferSource to ArrayBuffer-only views — the libsodium
// outputs are typed Uint8Array<ArrayBufferLike> which technically allows
// SharedArrayBuffer-backed slices (impossible at runtime here, but the
// type-checker can't see that). Returning the explicit ArrayBuffer view
// satisfies the SubtleCrypto signatures.
function copyToArrayBuffer(src: Uint8Array): ArrayBuffer {
  const buf = new ArrayBuffer(src.byteLength);
  const view = new Uint8Array(buf);
  view.set(src);
  return buf;
}

// deriveIdentity runs the full pipeline: Argon2id over (passphrase, salt)
// → 32-byte root seed → HKDF-SHA256 split into box_seed + sign_seed →
// (crypto_box_seed_keypair, crypto_sign_seed_keypair). Throws when the
// passphrase is shorter than MIN_IDENTITY_PASSPHRASE_LEN so callers can
// branch on the message before submitting anything to the server.
//
// Caller MUST `await ready()` first; this function does not auto-await
// to keep the failure mode loud — a missing `ready()` produces a clear
// libsodium error rather than a silent zero-output keypair.
export async function deriveIdentity(
  passphrase: string,
  username: string,
): Promise<DerivedIdentity> {
  if (passphrase.length < MIN_IDENTITY_PASSPHRASE_LEN) {
    throw new Error(
      `identity passphrase must be at least ${String(MIN_IDENTITY_PASSPHRASE_LEN)} characters`,
    );
  }
  const salt = await identitySalt(username);
  const enc = new TextEncoder();

  const rootSeed = sodium.crypto_pwhash(
    ARGON_KEY_LEN,
    passphrase,
    salt,
    ARGON_TIME,
    ARGON_MEMORY_BYTES,
    sodium.crypto_pwhash_ALG_ARGON2ID13,
  );

  const boxSeed = await hkdfDeriveBits(rootSeed, enc.encode(HKDF_INFO_BOX), 32 * 8);
  const signSeed = await hkdfDeriveBits(rootSeed, enc.encode(HKDF_INFO_SIGN), 32 * 8);

  const boxKp = sodium.crypto_box_seed_keypair(boxSeed);
  const signKp = sodium.crypto_sign_seed_keypair(signSeed);

  return {
    rootSeed,
    boxSeed,
    signSeed,
    boxPub: boxKp.publicKey,
    boxPriv: boxKp.privateKey,
    signPub: signKp.publicKey,
    signPriv: signKp.privateKey,
  };
}

// b64 is a thin re-export so callers can encode the raw 32-byte pubkeys
// for the wire without pulling sodium into their own module. Wraps
// libsodium's variant-base64 helper using the standard (non-URL-safe)
// alphabet to match the server's encoding/base64.StdEncoding.
export function b64(raw: Uint8Array): string {
  return sodium.to_base64(raw, sodium.base64_variants.ORIGINAL);
}
