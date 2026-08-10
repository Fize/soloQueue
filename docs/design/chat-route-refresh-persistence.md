# Chat Route Refresh Persistence — Historical Design Note

> This is an implementation record, not a user guide. For current behavior,
> start at [the documentation hub](../README.md).

## Problem

The desktop working indicator loses the active task level and model after a renderer refresh because route metadata currently exists only in the in-memory Zustand store. Persisting a route must not allow an older request to affect a later request.

## Approach

Persist the current in-flight `ChatRouteInfo` map in the existing browser `localStorage`. Initialize the store from that map, replace the entry whenever `setRoute` receives a route, and remove it when `clearRoute` removes the matching request. When runtime state restores an active session, reuse persisted metadata only when its `requestId` matches the runtime request.

## Dependencies

Existing Zustand store, browser `localStorage`, and Vitest tests. No new dependencies or backend protocol changes.

## Test Cases

- [x] A route remains available after the store is reinitialized from `localStorage`.
- [x] A new route overwrites an older route for the same session.
- [x] Clearing the matching request removes the persisted route, while a stale clear cannot remove a newer request.
- [x] Runtime recovery does not reuse route metadata belonging to a different request.
- [x] An idle runtime state clears a route left behind by a renderer refresh.

## Explicitly Out of Scope

Persisting completed messages, streaming content, request handlers, or backend runtime metadata is out of scope.
