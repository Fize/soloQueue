# Responsive workspace capabilities

## Decision

The Web Console uses the effective viewport (`window.innerWidth` and
`window.innerHeight`) as its first responsive workspace signal. User-agent
detection is intentionally not used. The classifier is shared by the Chat
input and Chat page so the Design Mode boundary has one source of truth.

| Workspace | Effective viewport | Design Mode | Chat/design layout |
| --- | --- | --- | --- |
| Phone | `width < 720` **or** `height < 600` | unavailable | no design surface |
| Pad | `width >= 720`, `height >= 600`, `width < 1200` | available | single pane when `width < 1000`; split pane when `width >= 1000` |
| Desktop | `width >= 1200`, `height >= 700` | available | existing split pane |

The height guard is deliberate: a short viewport must not enter an editable
Design Mode merely because its width resembles a tablet or desktop. A
viewport that falls between the named bands because it is at least 1200px
wide but shorter than 700px is treated as phone-capability (Design Mode is
disabled) until it satisfies the Desktop height requirement.

## State transitions

- Entering Phone while Design Mode is active automatically calls the existing
  runtime-store setter with `false`.
- The transition only changes the mode flag and its local persistence. It does
  not delete, clear, rewrite, or reset design files, preview HTML, or drawing
  data.
- Returning to Pad or Desktop does not automatically re-enter Design Mode;
  the user may enable it again from the input when the capability is available.
- Resize and orientation changes recalculate the capability immediately.

## Page impact in this slice

- `ChatInput` hides the Design Mode control on Phone and retains the existing
  L1 restriction.
- `ChatPage` uses a full-width single-pane design surface for Pad portrait and
  the existing horizontal split for Pad landscape and Desktop. The resize
  affordance is omitted in single-pane mode.
- `ChatDesignPanel` keeps its existing callbacks, tabs, preview, and drawing
  persistence. It is only rendered while the runtime mode is active.

## Out of scope

This slice does not add a PWA manifest, service worker, install prompt,
offline cache, backend/API behavior, or responsive migration of unrelated
pages. Those require a separate product and operational decision.
