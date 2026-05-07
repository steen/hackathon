import type * as React from "react";
import "./ChannelsList.css";

export interface Channel {
  id: string;
  name: string;
}

interface Props {
  channels: Channel[];
  activeId: string | null;
  onSelect: (id: string) => void;
  loading?: boolean;
  error?: string | null;
}

export function ChannelsList({
  channels,
  activeId,
  onSelect,
  loading,
  error,
}: Props): React.JSX.Element {
  return (
    <>
      {loading === true ? <p>Loading...</p> : null}
      {error !== null && error !== undefined ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : null}
      <ul aria-label="Channels" className="channels-list">
        {channels.map((c) => (
          <li key={c.id}>
            <button
              type="button"
              onClick={() => {
                onSelect(c.id);
              }}
              aria-current={c.id === activeId ? "true" : undefined}
            >
              #{c.name}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}
