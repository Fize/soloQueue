# Channel Image Delivery Fix — Design

## Problem

WeChat may deliver text and image parts as separate events, while QQ and WeChat currently omit saved image metadata from the timeline. Assistant-produced images can also point outside the server's previewable roots.

## Approach

Buffer only complementary WeChat text-only and media-only messages from the same immutable route for up to one second, with structured timing logs. Reuse `FilesContextKey` for channel attachments, keep media-bearing turns out of the string-only pending queue, and copy otherwise unpreviewable `SendFile` inputs into the existing artifacts directory.

## Dependencies

Use only existing Go packages and standard-library `sync`, `time`, and filesystem primitives. Reuse the existing channel route key, `FilesContextKey`, `WithRejectBusyQueue`, timeline file fields, and file-serving allowlist.

## Test Cases

- [ ] WeChat merges text followed by media from the same route within one second.
- [ ] WeChat merges media followed by text from the same route within one second.
- [ ] WeChat does not merge text with text, media with media, different routes, or messages outside the one-second window.
- [ ] WeChat built-in commands bypass buffering.
- [ ] WeChat merge and timeout paths emit timing logs without raw content or reply tokens.
- [ ] QQ and WeChat saved images populate `FilesContextKey` and timeline file metadata.
- [ ] Media-bearing channel turns wait with their structured context instead of entering the string-only pending queue.
- [ ] `SendFile` preserves its result shape and copies only otherwise unpreviewable local files into the existing artifacts directory.
- [ ] Existing QQ routing, WeChat routing, text pending, media sending, and visual-model behavior remain unchanged.

## Explicitly Out of Scope

- No database migration or artifact registry.
- No new file API or Desktop component redesign.
- No QQ message buffering.
- No unification of QQ and WeChat outbound protocol, rate limiting, typing, ASR, or command handling.
- No general rewrite of the pending queue or public `AskStream` API.
