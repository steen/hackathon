### Web

- Self-authored chat messages are no longer announced by screen readers. Optimistic-send rows whose `sender_user_id` matches the current user carry `aria-hidden="true"`, so the messages-list `role="log"` polite region (added in #454) stops reading the user's own typed text back to them. Failed-status rows stay announceable so the failed-badge `role="status"` and Retry control remain in the a11y tree. (#573, closes #468)
