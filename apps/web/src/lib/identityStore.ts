// Phase-10 IndexedDB persistence for the derived identity root seed.
// Decision-log §4 + L11: web stores the equivalent of `~/.config/chatd/
// identity.seed` so day-2 visits don't re-derive Argon2id from the
// passphrase. The seed is the 32-byte Argon2id output; sub-seeds and
// keypairs are recomputable from it via deriveIdentity()'s HKDF split,
// so we never store the keypairs themselves.
//
// Failure mode: a browser without IndexedDB (Safari private mode used
// to disable it; Firefox + chrome blocked-cookies similar) falls
// through to in-memory only. The caller treats writeIdentitySeed
// errors as recoverable and proceeds — a refreshed page will re-prompt.

const DB_NAME = "snakd-identity";
const DB_VERSION = 1;
const STORE_NAME = "seed";
const RECORD_KEY = "current";

interface SeedRecord {
  user_id: string;
  seed: Uint8Array;
}

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === "undefined") {
      reject(new Error("indexedDB is not available"));
      return;
    }
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = (): void => {
      req.result.createObjectStore(STORE_NAME);
    };
    req.onsuccess = (): void => {
      resolve(req.result);
    };
    req.onerror = (): void => {
      reject(req.error ?? new Error("indexedDB.open failed"));
    };
  });
}

export async function writeIdentitySeed(userId: string, seed: Uint8Array): Promise<void> {
  const db = await openDb();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readwrite");
    const store = tx.objectStore(STORE_NAME);
    // Copy out of the libsodium-owned buffer; libsodium reuses internal
    // memory and overwriting the seed in place after we have stored a
    // reference would corrupt the persisted record.
    const record: SeedRecord = { user_id: userId, seed: new Uint8Array(seed) };
    store.put(record, RECORD_KEY);
    tx.oncomplete = (): void => {
      resolve();
    };
    tx.onerror = (): void => {
      reject(tx.error ?? new Error("indexedDB write failed"));
    };
  });
  db.close();
}

export async function readIdentitySeed(): Promise<SeedRecord | null> {
  const db = await openDb();
  const out = await new Promise<SeedRecord | null>((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readonly");
    const store = tx.objectStore(STORE_NAME);
    const req = store.get(RECORD_KEY);
    req.onsuccess = (): void => {
      const v = req.result as SeedRecord | undefined;
      resolve(v ?? null);
    };
    req.onerror = (): void => {
      reject(req.error ?? new Error("indexedDB read failed"));
    };
  });
  db.close();
  return out;
}

export async function clearIdentitySeed(): Promise<void> {
  const db = await openDb();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readwrite");
    const store = tx.objectStore(STORE_NAME);
    store.delete(RECORD_KEY);
    tx.oncomplete = (): void => {
      resolve();
    };
    tx.onerror = (): void => {
      reject(tx.error ?? new Error("indexedDB clear failed"));
    };
  });
  db.close();
}
