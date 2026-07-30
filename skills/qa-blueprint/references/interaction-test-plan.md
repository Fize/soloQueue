# Interaction Test Plan

When the user needs E2E or UI interaction testing, generate a detailed plan BEFORE executing any tests. This prevents:

- Missing interactive behaviors (validation, loading, errors, empty states)
- Testing only happy paths
- Overlooking accessibility or keyboard interactions
- Undefined pass/fail criteria for each test

## When to Activate

- User says: E2E, 交互测试, UI testing, browser testing, 端到端测试, functional test plan
- Recommended test types include E2E
- Project has a user-facing UI (web, mobile, desktop)
- User asks "how do I test this UI"

## Phase 1: Feature Inventory

List EVERY user-facing feature, page, or flow. Do not skip anything a user can see or interact with.

### Discovery Methods

1. **Read route definitions**: `app/routes/`, `pages/`, `src/router/`, URL patterns
2. **Read component tree**: Entry components, page components, shared components
3. **Read navigation**: Menus, tabs, breadcrumbs, sidebar items
4. **Read API endpoints**: What data drives each page (forms, lists, detail views)
5. **Ask the user**: "Are there any features or workflows I haven't discovered?"

### Output: Feature List

```
1. Login page (/login)
2. Registration page (/register)
3. Dashboard (/dashboard)
4. User profile (/profile)
5. Settings (/settings)
6. ...
```

## Phase 2: Element Registry

For each feature, enumerate ALL interactive elements:

| Element Type | Examples | What to Document |
|-------------|----------|-----------------|
| Buttons | Submit, Cancel, Delete, Edit, Save | Label, action, disabled state |
| Text inputs | Email, Password, Search, Name | Placeholder, validation rules, input type |
| Textareas | Description, Comment, Bio | Char limit, auto-resize behavior |
| Selects/Dropdowns | Country, Role, Category | Options, default, multi-select support |
| Checkboxes | "Remember me", "Agree to terms" | Default state, required flag |
| Radio groups | Payment method, Shipping option | Options, default selection |
| Toggles/Switches | Dark mode, Notifications | On/off states, persistence |
| Date/Time pickers | Birthdate, Appointment | Format, range limits |
| File uploads | Avatar, Attachment | Accepted types, size limit, preview |
| Modals/Dialogs | Confirm delete, Edit details | Trigger, close methods (X, Escape, backdrop click) |
| Tooltips | Info icons, Help text | Trigger (hover/click), content |
| Drag handles | Reorder list, Resize panel | Drop targets, animation |
| Tabs | Content sections | Active indicator, lazy loading |
| Accordions | FAQ, Sections | Expand/collapse behavior |
| Infinite scroll | Feeds, Lists | Load more trigger, loading indicator |
| Search/Autocomplete | Lookup fields | Debounce, results dropdown, keyboard navigation |

## Phase 3: Behavior Catalog

For each element, document ALL behavioral states:

### Universal States (check every element)

1. **Happy path** — Correct input → expected output
2. **Validation feedback** — Invalid input → error message + visual indicator
   - Required field left empty
   - Format violation (email, phone, URL)
   - Range violation (min/max length, value)
   - Type mismatch
3. **Loading state** — What shows while data/action is in progress
   - Spinner or skeleton
   - Button disabled state
   - Progress indicator for long operations
4. **Empty state** — What shows when there is no data
   - Empty state illustration + message
   - Call to action (if applicable)
5. **Error state** — What shows when something fails
   - API error → error message or toast
   - Network error → retry option
   - Permission error → appropriate messaging
6. **Edge cases** — Unusual but valid inputs
   - Very long input (names, descriptions)
   - Special characters (Unicode, emoji)
   - Boundary values (0, -1, very large numbers)
   - Whitespace-only input
   - Leading/trailing spaces

### Interaction-Specific Behaviors

| Element | Extra Checks |
|---------|-------------|
| Form submission | Double-submit prevention, unsaved changes warning |
| Modal/Dialog | Focus trap, Escape to close, backdrop click to close, scroll lock |
| Dropdown/Select | Keyboard navigation (Arrow keys, Enter, Escape), option filtering |
| Drag and drop | Visual feedback during drag, valid drop zones, cancel behavior |
| File upload | Drag-to-upload zone, progress bar, cancel upload, invalid file feedback |
| Search | Debounce timing, clear button, "no results" state, keyboard shortcut |

## Phase 4: Test Plan Output

```markdown
## Interaction Test Plan: {project}

### Feature: {feature name} ({route})

| # | Element | Action | Expected Behavior | State Type |
|---|---------|--------|-------------------|------------|
| 1 | Email input | Leave empty, click Submit | Shows "Email is required" error, red border | Validation |
| 2 | Email input | Type "not-an-email" | Shows "Invalid email format" | Validation |
| 3 | Email input | Type "user@example.com" | No error, green checkmark | Happy path |
| 4 | Password input | Type 5 characters | Shows "Minimum 8 characters" | Validation |
| 5 | Password input | Type 8+ characters | No error | Happy path |
| 6 | Submit button | Click with valid form | Button shows spinner, fields disabled | Loading |
| 7 | Submit button | Click, API returns 500 | Shows toast "Something went wrong. Try again." | Error |
| 8 | Submit button | Click twice rapidly | Second click ignored (disabled state) | Edge case |
| 9 | Password input | Paste text with leading space | Space trimmed before validation | Edge case |
| 10 | Login form | Tab through all fields | Focus order: Email → Password → Submit → Forgot password | Keyboard |

### Feature: {next feature}
...
```

## Phase 5: Execution Handoff

After generating the test plan, provide execution instructions:

```
To execute this test plan:
1. For automated browser testing: invoke the webapp-testing skill with this plan
2. For interactive browser automation: invoke the Agent Browser skill with this plan
3. For manual testing: use this plan as a checklist

Each row in the plan is one test case with a defined pass/fail criterion.
```

## Remember

- **Happy path is the minimum, not the plan.** Every element has at least 3 states.
- **Validation states are often the most bug-prone** — give them extra attention.
- **Ask the user** if there are features or edge cases you haven't discovered.
- **The plan goes to webapp-testing or Agent Browser for execution.** This skill owns the strategy; they own the tooling.
