import type * as React from "react";
import "./Sidebar.css";

interface Props {
  /** Top-of-sidebar content; typically the username + sign-out controls. */
  header: React.ReactNode;
  children: React.ReactNode;
}

export function Sidebar({ header, children }: Props): React.JSX.Element {
  return (
    <aside className="sidebar" aria-label="Chat sidebar">
      <header>{header}</header>
      {children}
    </aside>
  );
}
