import type * as React from "react";

interface Props {
  text: string;
}

// Hidden polite live region for join/leave announcements. Mirrors the
// previous inline element in Chat.tsx 1:1 so existing E2E selectors
// (`[data-testid=presence-live-region]`) keep resolving. The
// `.visually-hidden` class lives in the consumer's global stylesheet.
export function PresenceLiveRegion({ text }: Props): React.JSX.Element {
  return (
    <div
      className="visually-hidden"
      aria-live="polite"
      aria-atomic="true"
      data-testid="presence-live-region"
    >
      {text}
    </div>
  );
}
