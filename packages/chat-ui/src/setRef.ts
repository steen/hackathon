import type { MutableRefObject, Ref } from "react";

// Forwards a value to a parent's `Ref<T>` of either function or object form.
// Used by components that own an internal ref but accept a parent ref prop
// for cross-component focus / scroll orchestration (MessageList listRef,
// MessageComposer composerRef).
export function setRef<T>(ref: Ref<T> | undefined, value: T | null): void {
  if (typeof ref === "function") {
    ref(value);
  } else if (ref !== null && ref !== undefined) {
    (ref as MutableRefObject<T | null>).current = value;
  }
}
