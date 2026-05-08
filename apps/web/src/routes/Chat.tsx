import type * as React from "react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  ChannelHeader,
  ChannelsList,
  MESSAGE_MAX_BYTES,
  MessageComposer,
  MessageList,
  PresenceList,
  PresenceLiveRegion,
  Sidebar,
  TopBar,
} from "@hackathon/chat-ui";
import { useAuth } from "../auth/AuthContext.js";
import { useChannels } from "../hooks/useChannels.js";
import { useChatSocket } from "../hooks/useChatSocket.js";
import { useMessages } from "../hooks/useMessages.js";
import { usePresence } from "../hooks/usePresence.js";
import { ChannelCreateModal } from "../components/ChannelCreateModal.js";
import { ChannelRenameModal } from "../components/ChannelRenameModal.js";

export function Chat(): React.JSX.Element {
  const { user, logout } = useAuth();
  const [activeChannel, setActiveChannel] = useState<string | null>(null);
  // Single chat-page socket. Both useMessages (message frames + reconnect
  // catchup) and useChannels (channel-create/rename frames + reload-on-open)
  // attach listeners to the same WebSocketClient.
  const sharedSocket = useChatSocket(activeChannel);
  const channelsState = useChannels(true, { socket: sharedSocket.socket });
  const messagesState = useMessages(activeChannel, user?.id ?? null, sharedSocket);
  const presenceState = usePresence(true, activeChannel);
  const [draft, setDraft] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const listRef = useRef<HTMLDivElement | null>(null);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const headingRef = useRef<HTMLHeadingElement | null>(null);
  // Once the focus anchor lands on the composer, leave it alone — re-running
  // the effect on later state changes would steal focus from wherever the
  // user has navigated since (e.g. the channel list). The heading/list
  // branches are first-paint placeholders and remain re-targetable until the
  // composer becomes the resting anchor.
  const composerFocusedRef = useRef(false);

  useEffect(() => {
    if (activeChannel === null && channelsState.channels.length > 0) {
      setActiveChannel(channelsState.channels[0]?.id ?? null);
    }
  }, [activeChannel, channelsState.channels]);

  // Focus delivery, priority composer → heading → list. useLayoutEffect lands
  // focus before the browser paints so SR users don't see a frame on
  // `document.body`. Re-runs when `activeChannel` flips from null to a real
  // id (e.g. once `useChannels` resolves), promoting focus from the heading
  // placeholder to the composer. `composerFocusedRef` guards against
  // stealing focus back if the user has tabbed away since.
  useLayoutEffect(() => {
    if (composerFocusedRef.current) return;
    const composer = composerRef.current;
    if (composer !== null && activeChannel !== null) {
      composer.focus();
      composerFocusedRef.current = true;
      return;
    }
    const heading = headingRef.current;
    if (heading !== null) {
      heading.focus();
      return;
    }
    const list = listRef.current;
    if (list !== null) {
      list.focus();
    }
  }, [activeChannel]);

  // Build the polite-region announcement text from the latest presence
  // event. The presence list itself reorders rather than appends rows, so
  // SR users don't get an aria-live additions announcement from the list —
  // we mirror the event into a sibling status region instead. When the
  // username is unknown (live event for an id not in the seeded directory)
  // the phrase elides the id rather than reading out a UUID. The fallback
  // differs by kind: "a new user" reads naturally for joins but is
  // grammatically odd for leaves (the leaver isn't new from the listener's
  // frame), so unknown leaves drop "new" — see issue #495.
  const presenceAnnouncement = useMemo<string>(() => {
    const ev = presenceState.lastEvent;
    if (ev === null) return "";
    if (ev.username.length > 0) {
      return ev.kind === "join" ? `${ev.username} joined` : `${ev.username} left`;
    }
    return ev.kind === "join" ? "a new user joined" : "a user left";
  }, [presenceState.lastEvent]);

  // Auth user before presence: own messages render correctly even before
  // the /api/presence seed lands. Falls back to the raw id so an unknown
  // sender (history from a user who has since left) doesn't crash — #148.
  // Memoized so a future per-message memoized child can rely on a stable
  // reference identity across renders (#535).
  const resolveSender = useCallback(
    (id: string): string => {
      if (user !== null && user.id === id) return user.username;
      const known = presenceState.usernames.get(id);
      if (known !== undefined && known.length > 0) return known;
      return id;
    },
    [user, presenceState.usernames],
  );

  const activeChannelName = useMemo<string | null>(() => {
    if (activeChannel === null) return null;
    return channelsState.channels.find((c) => c.id === activeChannel)?.name ?? null;
  }, [activeChannel, channelsState.channels]);

  // Empty-state surfaces only after the initial channel fetch settles (no
  // loading, no error). Showing the "no channels" copy mid-load would race
  // the eventual list and flash for SR users.
  const showNoChannelsEmpty =
    !channelsState.loading && channelsState.error === null && channelsState.channels.length === 0;
  // Mirrors the no-channels guard above: hold the hint until the initial
  // listMessages fetch settles. Otherwise the connecting → connected window
  // (state is `messages === []`, `error === null`) flashes the hint for the
  // duration of the fetch on every channel switch.
  const showEmptyChannelHint =
    activeChannel !== null &&
    !messagesState.historyLoading &&
    messagesState.error === null &&
    messagesState.messages.length === 0;

  async function submitDraft(): Promise<void> {
    if (activeChannel === null) return;
    const body = draft.trim();
    if (body.length === 0) return;
    setDraft("");
    await messagesState.send(body);
  }

  return (
    <div className="chat-layout">
      {user !== null ? (
        <TopBar
          workspaceName="Hackathon"
          user={user}
          online={messagesState.connection === "open"}
          onSignOut={() => {
            void logout();
          }}
        />
      ) : null}
      <div className="chat-layout__body">
        <Sidebar>
          <div className="channels-header">
            <h2>Channels</h2>
            <button
              type="button"
              className="channels-header__create"
              onClick={() => {
                setCreateOpen(true);
              }}
            >
              + New channel
            </button>
          </div>
          <ChannelsList
            channels={channelsState.channels}
            activeId={activeChannel}
            onSelect={setActiveChannel}
            loading={channelsState.loading}
            error={channelsState.error}
          />
          <h2>Online</h2>
          <PresenceList users={presenceState.users} />
          {/* role="status" is owned by the TopBar's "Online" indicator, so
              the live region here drops it to avoid duplicate role-status
              elements (web.spec.ts queries by role=status, expects exactly
              one match). aria-atomic="true" so the SR re-reads the whole
              phrase on each event, not just the diff. */}
          <PresenceLiveRegion text={presenceAnnouncement} />
        </Sidebar>
        <main className="messages" aria-label={activeChannelName ?? "Messages"}>
          <div className="messages__header-row">
            <ChannelHeader channelName={activeChannelName} headingRef={headingRef} />
            {activeChannelName !== null && activeChannelName !== "general" ? (
              <button
                type="button"
                className="messages__rename"
                aria-label={`Rename channel ${activeChannelName}`}
                onClick={() => {
                  setRenameOpen(true);
                }}
              >
                Rename
              </button>
            ) : null}
          </div>
          <MessageList
            messages={messagesState.messages}
            resolveSender={resolveSender}
            selfUserId={user?.id ?? null}
            error={messagesState.error}
            showNoChannelsEmpty={showNoChannelsEmpty}
            showEmptyChannelHint={showEmptyChannelHint && activeChannelName !== null}
            emptyChannelHintText={
              activeChannelName !== null
                ? `This is the start of #${activeChannelName} — send a message to say hi.`
                : undefined
            }
            canLoadOlder={messagesState.canLoadOlder}
            isLoadingOlder={messagesState.isLoadingOlder}
            loadOlderError={messagesState.loadOlderError}
            onLoadOlder={() => {
              void messagesState.loadOlder();
            }}
            onRetry={(id) => {
              void messagesState.retry(id);
            }}
            listRef={listRef}
          />
          <MessageComposer
            value={draft}
            onChange={setDraft}
            onSubmit={() => {
              void submitDraft();
            }}
            disabled={activeChannel === null}
            maxBytes={MESSAGE_MAX_BYTES}
            placeholder={activeChannel === null ? "Select a channel first" : "Write a message..."}
            composerRef={composerRef}
          />
        </main>
      </div>
      <ChannelCreateModal
        open={createOpen}
        onClose={() => {
          setCreateOpen(false);
        }}
        onCreate={channelsState.create}
        onCreated={(ch) => {
          setActiveChannel(ch.id);
          // Reset the composer-focus latch so the layout effect re-runs
          // and lands focus on the composer of the freshly-selected
          // channel rather than wherever Modal restored it.
          composerFocusedRef.current = false;
        }}
      />
      <ChannelRenameModal
        open={renameOpen}
        onClose={() => {
          setRenameOpen(false);
        }}
        channelId={activeChannel}
        currentName={activeChannelName}
        onRename={channelsState.rename}
      />
    </div>
  );
}
