import type * as React from "react";
import "./Sidebar.css";

interface Props {
  children: React.ReactNode;
}

export function Sidebar({ children }: Props): React.JSX.Element {
  return (
    <aside className="sidebar" aria-label="Chat sidebar">
      {children}
    </aside>
  );
}
