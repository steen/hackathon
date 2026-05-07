// Picks a stable, distinct color for a username. Hash → hue, fixed
// saturation/lightness tuned for legibility against `--bg-app` (≥4.5
// AA contrast at lightness 70% across the hue wheel).
//
// Was a four-color class palette; with >4 distinct users the rotation
// inevitably collided ("user1 and user5 share blue"). HSL keys off
// the visible username (not the opaque ULID `sender_user_id`) so a
// rename — when usernames become editable — updates the color the way
// readers expect; collisions are bounded by the 360° hue wheel rather
// than a hardcoded modulus.
//
// FNV-1a was the first attempt; it produced clusters where adjacent
// suffixes ("user1", "user3") landed on adjacent hues (≤5° apart), so
// the rendered colors read as identical. cyrb53 — a small 53-bit
// non-cryptographic hash with strong avalanche — spreads adjacent
// inputs across the wheel.

function cyrb53(str: string, seed = 0): number {
  let h1 = 0xdeadbeef ^ seed;
  let h2 = 0x41c6ce57 ^ seed;
  for (let i = 0; i < str.length; i++) {
    const ch = str.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
  h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
  h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  // Fold both halves into a single 53-bit integer.
  return 4294967296 * (2097151 & h2) + (h1 >>> 0);
}

/**
 * Stable color string for the given visible name. OKLCH is
 * perceptually uniform, so a fixed lightness/chroma renders consistent
 * weight across the hue wheel; the same setting in HSL produces darker
 * blues than yellows. Hue is `cyrb53(name) % 360` directly — adjacent
 * inputs ("user1" vs "user3") land far apart because cyrb53 has strong
 * avalanche; collisions are bounded by the 360° hue wheel.
 */
export function userColor(name: string): string {
  const hue = cyrb53(name) % 360;
  return `oklch(78% 0.15 ${String(hue)})`;
}
