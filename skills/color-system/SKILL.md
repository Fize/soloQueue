---
name: color-system
description: |
  Generate curated color token systems with dark/light mode support.
  Pick from 5 battle-tested palettes (Neutral Modern, Linear Dark, Claude Warm,
  Cursor Code, Lovable Pop) or generate a custom palette from a base color.
  Output is a ready-to-paste tokens.css block with semantic variables,
  automatic dark-mode switching, and WCAG AA contrast validation.
when_to_use: "Trigger when the user mentions: color system, dark mode palette, light mode palette, color tokens, accessible colors, generate tokens.css"
  - "配色方案"
  - "深色模式"
  - "浅色模式"
---

# color-system

> Generate production-ready color token sets with dark/light mode support.

## What it does

This skill produces a complete `:root { … }` CSS block — ready to paste into the
first `<style>` of any artifact — containing semantic color tokens with automatic
dark-mode adaptation. Every palette ships with:

- **Light mode** as the default (`:root`)
- **Automatic dark mode** (`@media (prefers-color-scheme: dark)`)
- **Manual toggle** (`.dark` or `[data-theme="dark"]` class)
- **WCAG AA contrast validation** built into the workflow

## Side files

- `assets/tokens-template.css` — base CSS template with light/dark scaffolding.
- `references/palettes.md` — full curated palette catalog with all 5 systems'
  color values for both light and dark modes.

## Available palettes

Pick one keyword and the skill will emit the full token set instantly.

| Keyword   | Name               | Vibe                               | Best for                                  |
| --------- | ------------------ | ---------------------------------- | ----------------------------------------- |
| `neutral` | **Neutral Modern** | Clean, safe, neutral               | General products, dashboards, B2B tools   |
| `linear`  | **Linear Dark**    | Dark-native, precision engineering | Developer tools, SaaS, data-dense UIs     |
| `claude`  | **Claude Warm**    | Warm parchment, literary, human    | AI products, content platforms, editorial |
| `cursor`  | **Cursor Code**    | Warm light, code-editor aesthetic  | Developer tools, IDEs, technical products |
| `lovable` | **Lovable Pop**    | Creamy, playful, low-barrier       | No-code tools, creative apps, onboarding  |
| `custom`  | **Custom**         | Derived from your base color       | Brand-specific needs, experiments         |

## Workflow

1. **Ask the user** which palette they want (or if they want a custom one).
2. **If custom:** ask for a base hex color + mood (`warm` / `cool` / `neutral`).
3. **Read** `references/palettes.md` to look up the chosen curated palette.
4. **Generate** the `:root` block with light-mode values.
5. **Generate** the `@media (prefers-color-scheme: dark)` block with dark-mode overrides.
6. **Generate** the `.dark` class override for manual toggling.
7. **Validate** contrast ratios:
   - Body text (≤16 px) on background: **≥ 4.5:1**
   - Large text (>18 px or 14 px bold): **≥ 3:1**
   - UI components against adjacent surfaces: **≥ 3:1**
8. **Emit** the final CSS block. If any pair fails, adjust the offending color and re-validate.

## Dark-mode rules

Follow these rules when generating or adapting dark mode:

- **Never pure black** (`#000`) or pure white (`#fff`). Use `#0f0f0f` for dark backgrounds and `#f0f0f0` for dark foregrounds.
- **Preserve the accent hue** across modes. Only the surrounding neutrals flip.
- **Invert hover/active mix direction.** Light mode: `color-mix(in oklab, var(--accent), black 8%)`. Dark mode: `color-mix(in oklab, var(--accent), white 12%)`.
- **Semi-transparent white borders** on dark surfaces: `rgba(255,255,255,0.08)` for standard, `rgba(255,255,255,0.05)` for soft.
- **Desaturate semantic colors slightly** on dark (`--success`, `--warn`, `--danger`) to prevent neon glow.
- **Keep the same token names** in both modes. Only the values change.

## Token naming convention

Always use **semantic** names — never hue-based names:

```css
/* Good */
--bg: #fafafa;
--fg: #111111;
--accent: #2f6feb;
--success: #17a34a;

/* Bad — locks you out of theming */
--blue-500: #2f6feb;
--green-500: #17a34a;
```

### Standard token set

| Token             | Role                                                   |
| ----------------- | ------------------------------------------------------ |
| `--bg`            | Page canvas                                            |
| `--surface`       | Elevated cards, panels                                 |
| `--surface-warm`  | Tertiary surface tier (buttons, prominent interactive) |
| `--fg`            | Primary text                                           |
| `--fg-2`          | Secondary emphasis text                                |
| `--muted`         | Placeholders, metadata                                 |
| `--meta`          | Tertiary text, footnotes                               |
| `--border`        | Standard borders                                       |
| `--border-soft`   | Subtle dividers                                        |
| `--accent`        | Primary CTA, links, brand moments                      |
| `--accent-on`     | Text on accent background                              |
| `--accent-hover`  | Hover state for accent elements                        |
| `--accent-active` | Active/pressed state                                   |
| `--success`       | Positive state                                         |
| `--warn`          | Warning state                                          |
| `--danger`        | Error/destructive state                                |

## Custom palette generation

When the user asks for a custom palette:

1. Ask for a **base hex color** (e.g. `#7c3aed`).
2. Ask for a **mood**: `warm`, `cool`, or `neutral`.
3. Derive the full token set:
   - **Background**: mix the base color with white at 95–97% for light mode; mix with black at 90–95% for dark mode.
   - **Surface**: one step lighter than `--bg` in light mode; one step darker in dark mode.
   - **Foreground**: high-contrast complement. Warm mood → warm black (`#1a1a18`); cool mood → cool black (`#0f1115`); neutral → `#111111`.
   - **Muted**: foreground at ~45% opacity or a mid-tone gray in the same temperature family.
   - **Accent**: the base color itself, or a slightly more saturated variant.
   - **Semantic**: keep standard greens/yellows/reds but temperature-shift them to match the mood.
4. Run the contrast validation gate. Adjust if any pair fails.

## Accent discipline

The single biggest readability failure in AI-generated UIs is accent overuse.
Hard caps:

- **At most 2 visible uses of `--accent` per screen.**
- Typical pair: one eyebrow/chip + one primary CTA.
- Links count as accent; demote to `--fg` underline if you also have a CTA.
- Hover/focus rings count as accent. Ration accordingly.

## Output format

Emit the tokens as a single CSS block:

```css
:root {
  /* Light mode */
  --bg: …;
  --surface: …;
  /* … etc … */
}

@media (prefers-color-scheme: dark) {
  :root {
    /* Dark mode overrides */
    --bg: …;
    /* … etc … */
  }
}

.dark,
[data-theme="dark"] {
  /* Manual toggle — identical to media query */
  --bg: …;
  /* … etc … */
}
```

Do not invent token names outside the standard set. If a brand needs a
non-standard token, document it in a comment and keep it scoped to that
project only.
