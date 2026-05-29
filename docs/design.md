# Open Design System 自定义与模板指南

本指南包含 Open Design (OD) 内置设计体系的可复用规范，并提供一套完整、可运行的脚手架模板。你可以直接将本模板文件复制到你的自定义设计体系目录中，稍作修改即可快速运行。

---

## 1. 规范提取与设计原则 (Lens A & Lens B)

在自定义或导入新的设计体系时，为了能够顺利通过系统的 Guard 校验（`pnpm guard`），需要严格遵循以下规范：

### 结构正确性 (Lens A 核心要求)

- **9段式标题**：`DESIGN.md` 内必须按 `## 1.` 到 `## 9.` 顺序声明二级标题（前缀后可以添加自定义语境后缀）。
- **字体标签提取**：在 `Typography` (排版) 章节必须提供格式固定的字体提取段落（以 `Display:`, `Body:`, `Mono:` 开头）。
- **CSS 变量隔离**：在 `tokens.css` 中，所有的自定义 CSS 变量必须包裹在 `:root` 块中，暗色模式使用 `[data-theme="dark"]` 覆盖，严禁编写裸属性。
- **无占位符**：所有的色值必须为具体的 Hex 编码（如 `#625DF5`），绝不允许出现 `#REPLACE_ME` 或含有变量嵌套、不合法字符。

### 推理完整性 (Lens B 质量要求)

- **无障碍对比度**：前景色和背景色必须通过 **4.5:1**（常规文本）或 **3.0:1**（大文本）的无障碍对比度（WCAG AA 级标准）。
- **具体化的反模式**：`Anti-patterns` (反模式) 必须提供具有边界的绝对规范。例如：“不要使用圆角大于 4px”，而不是空洞的“不要过度设计”。
- **组件无硬编码**：在 `DESIGN.md` 的 Components 中编写示例 CSS 时，颜色属性必须通过 `var(--color-primary)` 的变量引用，严禁直接使用静态的十六进制颜色值。

---

## 2. 文件夹脚手架模板

在项目的 `open-design/design-systems/` 目录下创建一个新子文件夹（例如 `my-design-system/`），并将以下模板分别写入对应的文件中。

````carousel
```json
// manifest.json
{
  "schemaVersion": "od-design-system-project/v1",
  "id": "my-design-system",
  "name": "My Custom System",
  "category": "Modern & Minimal",
  "description": "A clean, custom design system for testing templates.",
  "source": {
    "type": "local",
    "path": "./design-systems/my-design-system"
  },
  "files": {
    "design": "DESIGN.md",
    "tokens": "tokens.css",
    "components": "components.html"
  }
}
````

<!-- slide -->

````markdown
# My Custom System

> Category: Modern & Minimal
> [在此处写一行对该设计系统的简介]

## 1. Visual Theme & Atmosphere

[在此处写入整体氛围描述，例如：冷静、工程感、少修饰、以内容和对比为主导]

## 2. Color Palette & Roles

- Background: `#FAF9F6`
- Foreground: `#1A1917`
- Accent: `#2B6CB0`
- Surface: `#FFFFFF`
- Border: `#E2E8F0`

## 3. Typography Rules

- Display font stack: "Inter", sans-serif
- Body font stack: "Inter", sans-serif
- Mono font stack: "JetBrains Mono", monospace

Font labels for catalog extraction:
Display: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif
Body: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif
Mono: "JetBrains Mono", ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace

## 4. Spacing

- Base space unit is 4px.
- Grid padding: 24px.
- Layout sections: 48px to 64px.

## 5. Layout & Composition

- 12-column responsive grid on desktop.
- 1200px max width container.
- Clean alignments and significant negative space as dividers.

## 6. Component Stylings

```css
.btn-primary {
  background: var(--color-accent);
  color: var(--color-surface);
  border-radius: var(--radius-base);
  padding: 8px 16px;
  font-size: 14px;
  font-weight: 500;
  border: none;
  cursor: pointer;
}
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: 24px;
}
```
````

## 7. Motion & Interaction

```css
.btn-primary:focus-visible {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  * {
    transition: none !important;
    animation: none !important;
  }
}
```

## 8. Voice & Brand

Minimalist, direct, focusing on precision and code telemetry.

## 9. Anti-patterns

- Do not use rounded corners greater than 6px for interactive buttons.
- Do not apply box shadows on inputs or buttons.
- Do not introduce secondary branding colors.

````
<!-- slide -->
```css
/* tokens.css */
:root {
  /* Colors */
  --color-bg: #FAF9F6;
  --color-text: #1A1917;
  --color-accent: #2B6CB0;
  --color-surface: #FFFFFF;
  --color-border: #E2E8F0;

  /* Typography */
  --font-sans: "Inter", -apple-system, sans-serif;
  --font-mono: "JetBrains Mono", monospace;

  /* Geometry */
  --radius-base: 4px;
  --radius-lg: 8px;
  --space-base: 4px;

  --transition-fast: 100ms ease-out;
}

[data-theme="dark"] {
  --color-bg: #11100E;
  --color-text: #F7FAFC;
  --color-accent: #63B3ED;
  --color-surface: #1E1D1B;
  --color-border: #2D3748;
}
````

<!-- slide -->

```html
<!-- components.html -->
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <title>Component Fixtures</title>
  </head>
  <body>
    <div class="card">
      <h3>Button Preview</h3>
      <button class="btn-primary">Click Me</button>
    </div>
  </body>
</html>
```

````

---

## 3. 本地验证与发布

当你新建完成上述文件后，你可以通过以下方式在 `soloQueue` 工程中本地调试或运行验证：

1. **注册与加载**：
   * 确认新建的设计体系存放于 `open-design/design-systems/<your-slug>/` 文件夹下。
   * 重启项目或刷新前端页面，顶栏的 **Design System** 下拉菜单会自动读取 `manifest.json` 并动态加载该系统。
2. **运行 manifest 校验**：
   在 `open-design/` 根目录下，运行命令行：
   ```bash
   pnpm exec tsx scripts/check-design-system-manifests.ts
   ```
   如果输出 `Design system manifest check passed...`，则代表你的设计体系成功合规。

---

````
