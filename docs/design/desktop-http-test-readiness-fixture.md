# Desktop HTTP Test Readiness Fixture — Design

## Problem

The shared HTTP transport waits for the backend readiness gate before calling `fetch`. Three older test suites mock `fetch` but leave the gate closed, so 20 tests hit Vitest's five-second timeout before their mocks can run.

## Approach

Keep the production readiness gate and all existing assertions. In each affected suite, explicitly initialize the existing connection store as backend-ready in `beforeEach`, matching the prerequisite those unit tests are not intended to exercise.

## Dependencies

- Existing Zustand connection store
- Existing Vitest and jsdom setup
- No new runtime or test dependency

## Test Cases

- [x] HTTP transport tests invoke their mocked `fetch` without waiting for backend readiness.
- [x] API wrapper tests invoke their mocked `fetch` without waiting for backend readiness.
- [x] Remote WebSocket token acquisition invokes its mocked HTTP request without waiting for backend readiness.
- [x] The complete Desktop Vitest suite no longer contains the 20 readiness-related timeouts.

## Explicitly Out of Scope

- Changing or bypassing the production readiness gate
- Increasing Vitest timeouts
- Globally forcing backend readiness for unrelated tests
- Addressing existing ESLint findings
