// Deterministic 4-color hashing for sender names. Matches the screenshot's
// 4-color palette (blue/green/purple/yellow); collisions are accepted.
const COLOR_CLASSES = [
  "msg__sender--user-blue",
  "msg__sender--user-green",
  "msg__sender--user-purple",
  "msg__sender--user-yellow",
] as const;

export type UserColorClass = (typeof COLOR_CLASSES)[number];

function hash(input: string): number {
  let h = 0;
  for (let i = 0; i < input.length; i++) {
    h = (h * 31 + input.charCodeAt(i)) | 0;
  }
  return Math.abs(h);
}

export function userColorClass(id: string): UserColorClass {
  return COLOR_CLASSES[hash(id) % COLOR_CLASSES.length] ?? COLOR_CLASSES[0];
}
