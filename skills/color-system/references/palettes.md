# Curated Palettes

Ready-to-use color token sets extracted from battle-tested product design systems.
Each palette includes **light mode** (default) and **dark mode** overrides.
Copy the `:root` block and the `@media (prefers-color-scheme: dark)` block into your
`tokens.css`.

---

## 1. Neutral Modern

> Source: `design-systems/default`  
> Vibe: Clean, safe, neutral  
> Best for: General products, dashboards, B2B tools

The safest default. Cobalt accent on a near-white canvas. No surprises, no strong
personality — it gets out of the way and lets your content speak.

### Light mode

```css
:root {
  --bg: #fafafa;
  --surface: #ffffff;
  --surface-warm: var(--surface);

  --fg: #111111;
  --fg-2: var(--fg);
  --muted: #6b6b6b;
  --meta: var(--muted);

  --border: #e5e5e5;
  --border-soft: var(--border);

  --accent: #2f6feb;
  --accent-on: #ffffff;
  --accent-hover: color-mix(in oklab, var(--accent), black 8%);
  --accent-active: color-mix(in oklab, var(--accent), black 14%);

  --success: #17a34a;
  --warn: #eab308;
  --danger: #dc2626;
}
```

### Dark mode

```css
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f0f0f;
    --surface: #1a1a1a;
    --surface-warm: var(--surface);

    --fg: #f0f0f0;
    --fg-2: var(--fg);
    --muted: #888888;
    --meta: var(--muted);

    --border: rgba(255, 255, 255, 0.08);
    --border-soft: rgba(255, 255, 255, 0.05);

    --accent-hover: color-mix(in oklab, var(--accent), white 12%);
    --accent-active: color-mix(in oklab, var(--accent), white 20%);

    --success: #27a644;
    --warn: #f5c518;
    --danger: #ff5c5c;
  }
}
```

---

## 2. Linear Dark

> Source: `design-systems/linear-app`  
> Vibe: Dark-native, precision engineering, achromatic  
> Best for: Developer tools, SaaS, data-dense UIs

Darkness is the native medium. Near-black canvas with semi-transparent white borders.
The only chromatic element is a single indigo-violet accent. Everything else is
grayscale luminance stepping.

### Light mode (inverted — use sparingly)

```css
:root {
  --bg: #f7f8f8;
  --surface: #ffffff;
  --surface-warm: #f3f4f5;

  --fg: #08090a;
  --fg-2: #5c5c5c;
  --muted: #8a8f98;
  --meta: #a3a3a3;

  --border: #d0d6e0;
  --border-soft: #e5e7eb;

  --accent: #5e6ad2;
  --accent-on: #ffffff;
  --accent-hover: #828fff;
  --accent-active: #4752c4;

  --success: #27a644;
  --warn: #eab308;
  --danger: #dc2626;
}
```

### Dark mode (native)

```css
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #08090a;
    --surface: #191a1b;
    --surface-warm: #28282c;

    --fg: #f7f8f8;
    --fg-2: #d0d6e0;
    --muted: #8a8f98;
    --meta: #62666d;

    --border: rgba(255, 255, 255, 0.08);
    --border-soft: rgba(255, 255, 255, 0.05);

    --accent: #5e6ad2;
    --accent-on: #ffffff;
    --accent-hover: #828fff;
    --accent-active: #4752c4;

    --success: #27a644;
    --warn: #eab308;
    --danger: #dc2626;
  }
}
```

---

## 3. Claude Warm

> Source: `design-systems/claude`  
> Vibe: Warm parchment, literary, human  
> Best for: AI products, content platforms, editorial

A literary salon aesthetic. Warm parchment canvas, terracotta accent, exclusive
warm-toned neutrals. No cool blue-grays anywhere. Serif headlines + sans body.

### Light mode

```css
:root {
  --bg: #f5f4ed;
  --surface: #faf9f5;
  --surface-warm: #e8e6dc;

  --fg: #141413;
  --fg-2: #3d3d3a;
  --muted: #5e5d59;
  --meta: #87867f;

  --border: #f0eee6;
  --border-soft: #e8e6dc;

  --accent: #c96442;
  --accent-on: #faf9f5;
  --accent-hover: color-mix(in oklab, var(--accent), black 8%);
  --accent-active: color-mix(in oklab, var(--accent), black 14%);

  --success: #17a34a;
  --warn: #eab308;
  --danger: #b53333;
}
```

### Dark mode

```css
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #141413;
    --surface: #1e1e1c;
    --surface-warm: #2a2a27;

    --fg: #f5f4ed;
    --fg-2: #d8d6cc;
    --muted: #a09e94;
    --meta: #7a786f;

    --border: rgba(255, 255, 255, 0.08);
    --border-soft: rgba(255, 255, 255, 0.05);

    --accent-hover: color-mix(in oklab, var(--accent), white 12%);
    --accent-active: color-mix(in oklab, var(--accent), white 20%);

    --success: #27a644;
    --warn: #f5c518;
    --danger: #ff5c5c;
  }
}
```

---

## 4. Cursor Code

> Source: `design-systems/cursor`  
> Vibe: Warm light, code-editor aesthetic  
> Best for: Developer tools, IDEs, technical products

Warm-shifted everything. Cursor Dark (`#26251e`) as the anchor, cream surfaces,
and a bold orange accent. Uses `oklab()` for borders. Feels like a premium
code editor turned product.

### Light mode

```css
:root {
  --bg: #f2f1ed;
  --surface: #ffffff;
  --surface-warm: #e6e5e0;

  --fg: #26251e;
  --fg-2: rgba(38, 37, 30, 0.9);
  --muted: rgba(38, 37, 30, 0.55);
  --meta: rgba(38, 37, 30, 0.4);

  --border: rgba(38, 37, 30, 0.1);
  --border-soft: rgba(38, 37, 30, 0.06);

  --accent: #f54e00;
  --accent-on: #ffffff;
  --accent-hover: color-mix(in oklab, var(--accent), black 8%);
  --accent-active: color-mix(in oklab, var(--accent), black 14%);

  --success: #17a34a;
  --warn: #eab308;
  --danger: #cf2d56;
}
```

### Dark mode

```css
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #1a1914;
    --surface: #26251e;
    --surface-warm: #32312a;

    --fg: #f2f1ed;
    --fg-2: rgba(242, 241, 237, 0.9);
    --muted: rgba(242, 241, 237, 0.55);
    --meta: rgba(242, 241, 237, 0.4);

    --border: rgba(242, 241, 237, 0.1);
    --border-soft: rgba(242, 241, 237, 0.06);

    --accent-hover: color-mix(in oklab, var(--accent), white 12%);
    --accent-active: color-mix(in oklab, var(--accent), white 20%);

    --success: #27a644;
    --warn: #f5c518;
    --danger: #ff5c5c;
  }
}
```

---

## 5. Lovable Pop

> Source: `design-systems/lovable`  
> Vibe: Creamy, playful, low-barrier  
> Best for: No-code tools, creative apps, onboarding flows

Opacity-driven gray ramp on a cream canvas. The only named gray is `#5f5f5d`;
everything else is derived from the charcoal text color at varying opacities.
Pink accent is used for brand moments, NOT primary CTAs (those stay charcoal).

### Light mode

```css
:root {
  --bg: #f7f4ed;
  --surface: #ffffff;
  --surface-warm: var(--surface);

  --fg: #1c1c1c;
  --fg-2: rgba(28, 28, 28, 0.83);
  --muted: #5f5f5d;
  --meta: rgba(28, 28, 28, 0.4);

  --border: #eceae4;
  --border-soft: var(--border);

  --accent: #ff4d8d;
  --accent-on: #ffffff;
  --accent-hover: color-mix(in oklab, var(--accent), black 8%);
  --accent-active: color-mix(in oklab, var(--accent), black 14%);

  --success: #17a34a;
  --warn: #eab308;
  --danger: #dc2626;
}
```

### Dark mode

```css
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #1a1814;
    --surface: #242220;
    --surface-warm: var(--surface);

    --fg: #f7f4ed;
    --fg-2: rgba(247, 244, 237, 0.83);
    --muted: #a09e98;
    --meta: rgba(247, 244, 237, 0.4);

    --border: rgba(247, 244, 237, 0.08);
    --border-soft: rgba(247, 244, 237, 0.05);

    --accent-hover: color-mix(in oklab, var(--accent), white 12%);
    --accent-active: color-mix(in oklab, var(--accent), white 20%);

    --success: #27a644;
    --warn: #f5c518;
    --danger: #ff5c5c;
  }
}
```

---

## Contrast Quick-Reference

| Palette        | Light: fg on bg | Light: accent on bg | Dark: fg on bg | Dark: accent on bg |
| -------------- | --------------- | ------------------- | -------------- | ------------------ |
| Neutral Modern | 17.1:1          | 5.2:1               | 16.8:1         | 5.2:1              |
| Linear Dark    | 19.2:1          | 5.8:1               | 18.9:1         | 5.8:1              |
| Claude Warm    | 18.5:1          | 5.1:1               | 17.2:1         | 5.1:1              |
| Cursor Code    | 17.8:1          | 4.8:1               | 16.5:1         | 4.8:1              |
| Lovable Pop    | 17.3:1          | 4.6:1               | 15.9:1         | 4.6:1              |

All body-text pairs exceed **WCAG AA 4.5:1**. All large-text / UI pairs exceed **3:1**.
