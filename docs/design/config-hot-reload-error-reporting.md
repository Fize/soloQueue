# Config Hot-Reload Error Reporting — Design

## Problem

The settings watcher silently discards YAML parse errors and still invokes the success callback. Operators can therefore mistake a rejected edit for an applied configuration while the process continues using the last valid snapshot.

## Approach

Add an error callback to the existing generic loader and connect it to the configuration logger. A failed reload reports the error and skips the success callback; the last valid snapshot remains active. Watcher-level errors use the same reporting path.

## Dependencies

- Existing `github.com/fsnotify/fsnotify` watcher
- Existing `gopkg.in/yaml.v3` parser
- Existing SoloQueue structured logger

## Test Cases

- [x] Invalid YAML reports the reload error, preserves the last valid settings, and does not invoke the success callback.
- [x] A subsequent valid write still reloads settings and invokes the success callback.

## Explicitly Out of Scope

- Retrying or debouncing transient file writes
- Adding UI notifications or new API endpoints
- Changing startup-time configuration error handling
- Replacing the last valid configuration with defaults after a reload failure
