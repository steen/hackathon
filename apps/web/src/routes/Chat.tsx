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
import { useDMs } from "../hooks/useDMs.js";
import { useMessages } from "../hooks/useMessages.js";
import { usePresence } from "../hooks/usePresence.js";
import { useReadMarker } from "../hooks/useReadMarker.js";
import { ChannelCreateModal } from "../components/ChannelCreateModal.js";
import { ChannelRenameModal } from "../components/ChannelRenameModal.js";
import { DMSidebar } from "../components/DMSidebar.js";
import { DMThread } from "../components/DMThread.js";
import { NewDMModal } from "../components/NewDMModal.js";

export function Chat(): React.JSX.Element {
  const { user, logout } = useAuth();
  const [activeChannel, setActiveChannel] = useState<string | null>(null);
  // Active surface — either a channel OR a DM thread, never both. The two
  // active-* states stay independent so flipping between them (sidebar
  // click) doesn't perturb the other; the rendered main pane keys on
  // `activeDM` so a DM selection wins over a stale activeChannel.
  const [activeDM, setActiveDM] = useState<string | null>(null);
  // Single chat-page socket. Both useMessages (message frames + reconnect
  // catchup) and useChannels (channel-create/rename frames + reload-on-open)
  // attach listeners to the same WebSocketClient.
  const sharedSocket = useChatSocket(activeChannel);
  const channelsState = useChannels(true, { socket: sharedSocket.socket });
  const dmsState = useDMs(true, { selfUserId: user?.id ?? null, socket: sharedSocket.socket });
  const messagesState = useMessages(activeChannel, user?.id ?? null, sharedSocket);
  const presenceState = usePresence(true, activeChannel);
  const [draft, setDraft] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [newDMOpen, setNewDMOpen] = useState(false);
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

  // Resolve the active DM conversation row for the renderer; the row
  // carries the peer summary plus unread baseline that DMThread needs.
  // Falls back to null when the active id was unmounted (e.g. a stale
  // selection after a hard refresh).
  const activeConversation = useMemo(() => {
    if (activeDM === null) return null;
    return dmsState.conversations.find((c) => c.id === activeDM) ?? null;
  }, [activeDM, dmsState.conversations]);

  // Sidebar mode-switch handlers. Selecting a channel clears the DM
  // selection (and vice versa) so the main pane has exactly one active
  // surface at a time. Reset the composer-focus latch on either switch
  // so focus re-lands on the freshly-mounted composer.
  const selectChannel = useCallback((id: string): void => {
    setActiveChannel(id);
    setActiveDM(null);
    composerFocusedRef.current = false;
  }, []);

  const selectDM = useCallback((id: string): void => {
    setActiveDM(id);
    composerFocusedRef.current = false;
  }, []);

  // Channel read-pointer advance (Phase 9 #873). The hook always runs (rules-
  // of-hooks); pass `null` when no channel is selected so the hook no-ops
  // its markRead/flush internals rather than POSTing to a sentinel id. The
  // hook flushes pending advances on visibility/focus return, so the
  // "focus return -> POST /read" leg is owned by useReadMarker — Chat.tsx
  // only feeds it the latest seen message id when the channel is active.
  const channelMarker = useReadMarker("channel", activeChannel);

  // Latest committed message id in the active channel. Optimistic-pending
  // rows have ULID-shaped client ids that the server has never seen, so
  // skip them — POST /read against an unknown id returns 404. Picking the
  // highest committed id from the in-view list is correct under the
  // server's advance-only semantic (older ids are 200 no-ops).
  //
  // Reverse scan over messages is bounded by the in-view window
  // (CATCHUP_LIMIT=50 plus loaded older pages, capped by server history).
  // Typical case is O(1): the tail row is committed (the last WS frame
  // was an inbound or self-echo, both committed), the loop returns on the
  // first iteration. Pathological case is a long suffix of pending/failed
  // self-sends, which only happens during outage windows. Maintaining an
  // O(1) lastCommittedRef would require threading updates through every
  // setMessages call site in useMessages — not worth the surface-area
  // expansion at current scale.
  const latestCommittedMessageId = useMemo<string | null>(() => {
    for (let i = messagesState.messages.length - 1; i >= 0; i -= 1) {
      const m = messagesState.messages[i];
      if (m === undefined) continue;
      if (m.status !== undefined) continue;
      return m.id;
    }
    return null;
  }, [messagesState.messages]);

  // Trigger the debounced markRead whenever the active channel has a new
  // latest committed message. The 250ms trailing debounce inside the hook
  // (decision-log L22) collapses bursts so a fast scroll past 30 rows
  // issues one POST. Visibility/focus return inside useReadMarker flushes
  // any pending advance immediately.
  const markChannelRead = channelMarker.markRead;
  useEffect(() => {
    if (activeChannel === null) return;
    if (latestCommittedMessageId === null) return;
    if (typeof document !== "undefined" && document.visibilityState === "hidden") {
      return;
    }
    markChannelRead(latestCommittedMessageId);
  }, [activeChannel, latestCommittedMessageId, markChannelRead]);

  // Empty-state surfaces only after the initial channel fetch settles (no
  // loading, no error). Showing the "no channels" copy mid-load would race
  // the eventual list and flash for SR users.
  const showNoChannelsEmpty =
    !channelsState.loading && channelsState.error === null && channelsState.channels.length === 0;
  // Mirrors the no-channels guard above: hold the hint until the initial
  // listMessages fetch settles. Otherwise the connecting → connected window
  // (state is `messages === []`, `error === null`) flashes the hint for the
  // duration of the fetch on every channel switch.
  //
  // useMessages's setHistoryLoading(true) lands in a useEffect that fires
  // post-commit, so the render where activeChannel first flips to a real
  // id has historyLoading=false and an empty messages array — all four
  // gate conditions hold for one frame. Track the channel we've seen the
  // hook acknowledge (via either historyLoading=true or a non-empty
  // messages array) and suppress the hint until that catches up. Adjusts
  // state during render per
  // https://react.dev/reference/react/useState#storing-information-from-previous-renders.
  const [acknowledgedChannel, setAcknowledgedChannel] = useState<string | null>(null);
  if (
    activeChannel !== null &&
    acknowledgedChannel !== activeChannel &&
    (messagesState.historyLoading || messagesState.messages.length > 0)
  ) {
    setAcknowledgedChannel(activeChannel);
  }
  const historySettledForActive = activeChannel !== null && acknowledgedChannel === activeChannel;
  const showEmptyChannelHint =
    activeChannel !== null &&
    historySettledForActive &&
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
            activeId={activeDM === null ? activeChannel : null}
            onSelect={selectChannel}
            loading={channelsState.loading}
            error={channelsState.error}
          />
          <DMSidebar
            conversations={dmsState.conversations}
            activeId={activeDM}
            onSelect={selectDM}
            onNew={() => {
              setNewDMOpen(true);
            }}
            loading={dmsState.loading}
            error={dmsState.error}
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
        {activeConversation !== null ? (
          <DMThread
            conversation={activeConversation}
            selfUserId={user?.id ?? null}
            resolveSender={resolveSender}
            socket={sharedSocket.socket}
          />
        ) : (
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
        )}
      </div>
      <ChannelCreateModal
        open={createOpen}
        onClose={() => {
          setCreateOpen(false);
        }}
        onCreate={channelsState.create}
        onCreated={(ch) => {
          // selectChannel switches to the new channel id, clears any DM
          // selection, and resets the composer-focus latch so the
          // layout effect re-runs and lands focus on the composer of
          // the freshly-selected channel.
          selectChannel(ch.id);
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
      <NewDMModal
        open={newDMOpen}
        onClose={() => {
          setNewDMOpen(false);
        }}
        selfUserId={user?.id ?? null}
        onCreate={dmsState.startWith}
        onCreated={(conv) => {
          selectDM(conv.id);
        }}
      />
    </div>
  );
}
