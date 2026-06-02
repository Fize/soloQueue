# SoloQueue Desktop — 像素模拟经营系统设计文档

## 一、项目概述

将 soloQueue 的 Web UI 重构为 Electron 桌面应用，外壳采用 **8-bit 像素风模拟经营游戏** 风格，视觉参考《Katana Zero》。用户启动后看到办公楼外观 → 点击开始 → 进入办公层内部，以游戏化方式管理和监控多 Agent 系统。

---

## 二、技术栈

| 层 | 技术 |
|---|------|
| 桌面壳 | Electron + electron-vite |
| 前端框架 | React 19 + TypeScript |
| 像素场景渲染 | react-konva（Canvas 2D） |
| 动画 | react-konva 内置 tweens + CSS animations |
| 状态管理 | zustand（复用 web store 模式） |
| 样式 | TailwindCSS v4（UI 面板部分） |
| 后端通信 | WebSocket（复用） + REST（复用） |
| Go 后端 | 作为 Electron 子进程 spawn |
| 打包 | electron-builder |

---

## 三、视觉风格

### 3.1 参考：《Katana Zero》

- 暗色 noir 基调，深紫黑背景
- 暖黄色灯光作点缀照明
- 霓虹青/品红作为交互高亮色
- 像素颗粒感强，粗边框，锯齿边缘
- 室内场景以房间/隔间划分空间

### 3.2 配色方案

```
┌──────────┬──────────┬──────────────────────────┐
│ 角色     │ 色值     │ 用途                     │
├──────────┼──────────┼──────────────────────────┤
│ 背景深色 │ #0f0f1a  │ 全局背景、窗外夜景       │
│ 暗区     │ #1a1a30  │ 未激活区域、墙体暗面     │
│ 建筑中调 │ #2a2845  │ 地板、隔断墙             │
│ 亮表面   │ #3d3a58  │ 桌面、面板背景           │
│ 暖灯光   │ #e8b450  │ 吊灯、台灯、交互高亮     │
│ 霓虹青   │ #50e0c0  │ Agent 运行中、成功状态   │
│ 霓虹橙   │ #f08040  │ 警告、超时、阻塞         │
│ 霓虹品红 │ #e05060  │ 错误、异常、宕机         │
│ 文字暖白 │ #e0d8c8  │ 主文字                   │
│ 文字暗色 │ #8a8a9a  │ 次要文字、不可用项       │
│ 像素边框 │ #4a4a68  │ 像素面板边框             │
│ 半透遮罩 │ rgba(0,0,0,0.6) │ 弹窗遮罩        │
└──────────┴──────────┴──────────────────────────┘
```

### 3.3 像素比例

- 画面基础像素单位：4px（屏幕 1 像素 = 1/4 游戏像素，放大 4 倍渲染）
- 所有 UI 元素对齐 4px 网格
- 文字使用像素字体，字号必须是 4 的倍数

---

## 四、场景设计

### 4.1 标题画面（Title Scene）

```
┌──────────────────────────────────────────────┐
│                                              │
│          ░░░░░░░░░ (粒子/雨)                 │
│                                              │
│          ┌──────────────────┐                │
│          │                  │                │
│          │   办公楼外观      │                │
│          │   像素绘制        │                │
│          │   窗户明暗交替    │                │
│          │   霓虹招牌闪烁    │                │
│          │                  │                │
│          └──────────────────┘                │
│                                              │
│          SOLOQUEUE INC.                      │
│          Est. 2026                            │
│                                              │
│       ┌────────────────────┐                 │
│       │   ▶  S T A R T    │                 │
│       └────────────────────┘                 │
│                                              │
│          v0.1.0    ⚙ Settings               │
└──────────────────────────────────────────────┘
```

**素材需求**：
- 办公楼外观图 → **需要像素素材**（可先用 CSS 搭建占位，后续替换）
- 霓虹招牌字 "SOLOQUEUE" → **CSS 文字 + glow 滤镜**
- 雨/粒子背景 → **Canvas 程序化绘制**
- 开始按钮 → **CSS 像素按钮**

### 4.1-A 角色创建画面（Character Creation Scene）— 首次启动

> **触发条件**：后端返回 soul/profile 文件不存在（首次启动）
> **目的**：创建 L1 首席秘书角色（性别、名称、对话风格）

```
┌──────────────────────────────────────────────┐
│                                              │
│          ┌──────────────────────┐            │
│          │   NEW EMPLOYEE       │            │
│          │   REGISTRATION       │            │
│          └──────────────────────┘            │
│                                              │
│     Step 1/3: 选择性别                       │
│     ┌─────────────┐  ┌─────────────┐        │
│     │  ♂ 男性     │  │  ♀ 女性     │        │
│     │  [预览]     │  │  [预览]     │        │
│     └─────────────┘  └─────────────┘        │
│                                              │
│     ┌──────────────────────────────┐         │
│     │      人物像素预览 (48×48)     │         │
│     │      实时显示选择结果         │         │
│     └──────────────────────────────┘         │
│                                              │
│     ┌──────────┐       ┌───────────┐        │
│     │  ← 返回  │       │  下一步 → │        │
│     └──────────┘       └───────────┘        │
└──────────────────────────────────────────────┘

     Step 2/3: 设定名称
     ┌──────────────────────────────┐         │
     │  请输入你的名字：              │         │
     │  ┌────────────────────────┐  │         │
     │  │ Alex █                 │  │         │
     │  └────────────────────────┘  │         │
     └──────────────────────────────┘         │

     Step 3/3: 对话风格
     ┌───────────────────────────────────┐    │
     │  选择工作风格：                     │    │
     │  ○ 专业严谨 — 正式、准确、简洁      │    │
     │  ○ 友善热情 — 亲切、鼓励、详细      │    │
     │  ○ 幽默风趣 — 轻松、调侃、机智      │    │
     │  ○ 冷酷高效 — 冷淡、直接、不废话    │    │
     │  ○ 亦师亦友 — 教导、耐心、启发      │    │
     └───────────────────────────────────┘    │

     ┌──────────┐       ┌───────────┐        │
     │  ← 返回  │       │ ✓ 确认入职 │        │
     └──────────┘       └───────────┘        │
```

**交互逻辑**：
- 三步向导式创建，左侧显示步骤进度
- 右侧实时预览人物像素 Sprite（根据性别/风格切换表情）
- 确认后写入 soul/profile 配置，切入办公层场景

**素材需求**：
- 男/女像素人物预览（48×48 或更大）→ 🎨 需要
- CSS 像素 UI 面板全部 → 🖥️

---

### 4.2 办公层主场景（Office Scene）— 核心

```
┌──────────────────────────────────────────────┐
│  ┌──────────────────────────────────────┐    │
│  │  SOLOQUEUE INC.    09:41 PM    ⚙   │    │  ← 顶部 HUD
│  └──────────────────────────────────────┘    │
│                                              │
│  ┌───────────────┐ ┌──────────┐ ┌────────┐  │
│  │   Team A      │ │ Team B   │ │ Team C │  │  ← 团队隔间
│  │ ┌──┐┌──┐┌──┐ │ │┌──┐┌──┐│ │┌──┐   │  │
│  │ │🪑││🪑││🪑│ │ ││🪑││🪑││ ││🪑│   │  │  ← 空工位(无人)
│  │ │  ││  ││  │ │ ││  ││  ││ ││  │   │  │
│  │ └──┘└──┘└──┘ │ │└──┘└──┘│ │└──┘   │  │
│  │  ┌──看板──┐  │ │ ┌看板┐ │ │ ┌看板┐│  │  ← 部门看板
│  │  │ 📋    │  │ │ │📋  │ │ │ │📋  ││  │
│  │  └───────┘  │ │ └────┘ │ │ └────┘│  │
│  └───────┬─────┘ └───┬────┘ └───┬───┘  │
│          │            │          │       │
│     ┌────┴────────────┴──────────┴───┐   │  ← 走廊（寻路通道）
│     │       ═══ 主要走廊 ═══         │   │
│     └────────────────────────────────┘   │
│                                          │
│  ┌────────────┐          ┌────────────┐  │
│  │ L1 秘书工位│          │   🚪 入口  │  │  ← L1 常驻 + 入口
│  │  ┌──────┐  │          │  ELEVATOR  │  │
│  │  │ 👩‍💼  │  │          │  员工进出   │  │
│  │  │ L1   │  │          │  ──→ ←── │  │
│  │  └──────┘  │          └────────────┘  │
│  └────────────┘                          │
│                                          │
│  ┌──────────────────────────────────────┐│
│  │ 💰 24,520  📋 18 tasks  👥 3 active ││  ← 底部 HUD
│  └──────────────────────────────────────┘│
└──────────────────────────────────────────────┘
```

**核心机制 — Agent 调度生命周期**：

```
后台 idle（无实例）
    │
    │  收到任务 / 用户 Ask
    ↓
入口处 spawn（生成像素小人）
    │
    │  沿走廊 walk → 对应团队工位
    ↓
坐到工位 → 开始 working 动画
    │
    │  任务完成 / 超时 / 错误
    ↓
站起来 → 沿走廊 walk → 入口
    │
    ↓
入口处 despawn（像素小人消失）
```

**交互逻辑**：
- **点击空工位** → 无反应（无人）
- **点击工作中的 Agent** → 弹出 Agent 详情侧边栏（实时状态、对话流）
- **点击看板** → 切换到该团队 Kanban 视图
- **点击入口** → 查看当前调度队列（哪些 Agent 即将入场）
- **点击 L1 秘书** → L1 详情 + 全局系统概览
- **滚轮/拖拽** → 水平或垂直滚动

### 4.3 办公层布局规则

- **一层楼**，俯视平面视角（近似 RPG Maker 风格）
- **左下角**：办公楼入口（电梯/大堂），Agent 出生与消失点
- **右下角**：L1 首席秘书专属工位（始终在线，系统常驻）
- **上方**：各 Team 办公区，按团队用隔断墙分隔
- 每个 Team 区域包含：
  - 若干空工位（桌+椅+熄屏显示器，Agent 到达后点亮）
  - 墙壁上的看板（可交互）
  - Team 名称霓虹招牌
- **中间贯穿**：主走廊 → 支线走廊 → 连接每个 Team 入口
- 顶部/底部状态栏 HUD

**寻路规则**：
- 使用栅格化 A\* 寻路（Konva 场景划分 16×16 逻辑网格）
- 走廊和过道标记为 walkable，墙体/隔断/家具标记为 blocked
- Agent 移动速度：~64px/s（4 格/秒），动作游戏节奏感但不是实时跑动
- 同一团队的 L3 Agent 串行进入（排队走），不同团队可并行
- L2 Leader 优先于 L3 入场（如果有 Leader 被调度）

**渲染方式**：Konva Stage
- Layer 0：地板 tileset（程序化重复）
- Layer 1：墙体 / 隔断 / 走廊标记
- Layer 2：家具（桌子、椅子、显示器、看板、装饰物）
- Layer 3：Agent Sprite（动态，人物走在走廊或坐在工位）
- Layer 4：状态特效叠加（粒子、光环、烟雾）
- Layer 5：HUD 覆盖层

---

## 五、素材清单与绘制方式

### 5.1 分类标准

| 标记 | 含义 |
|------|------|
| 🎨 | 需要像素画师绘制（或 AI 生成后人工修） |
| 🖥️ | CSS 代码绘制（div + border + box-shadow） |
| 🎮 | Canvas 程序化绘制（Konva.Shape / Rect / Line） |
| 📝 | 纯文字/字体渲染 |

---

### 5.2 场景素材

| 素材 | 方式 | 说明 |
|------|------|------|
| 办公楼外观（标题画面） | 🎨 或 🖥️ | 先用 CSS 矩形搭建占位，后续替换为像素美术 |
| 办公楼入口/电梯 | 🎨 | Agent spawn 点，见 prompt A13 |
| 地板 tileset | 🎮 | Canvas 程序化生成，重复填充矩形 + 像素纹理 |
| 隔断墙 | 🎮 | Konva.Rect，纯色填充 + 像素边框 |
| 门 | 🎮 | Konva.Rect + 门把手（小圆点） |
| 窗户（带灯光） | 🎮 | Konva.Rect + 黄色发光矩形模拟窗口灯光 |
| 天花板吊灯 | 🎮 | Konva.Line（灯绳）+ Konva.Circle（灯泡）+ 暖光 radius 渐变 |
| 霓虹招牌 | 📝 + 🖥️ | 像素字体文字 + CSS `text-shadow` glow 效果 |
| 雨水/粒子效果 | 🎮 | Canvas 粒子系统，程序化生成 |
| 桌面/办公桌 | 🎮 | Konva.Rect，像素棕色/灰色 |
| 办公椅 | 🎮 | 简化几何形状（矩形 + 小圆角矩形靠背） |
| 电脑显示器（熄屏/亮屏） | 🎮 | Konva.Rect（屏幕边框 + 内发光屏幕，状态切换） |
| 看板墙 | 🎮 | 墙壁矩形 + 小卡片缩略图 + 软木板纹理 |
| 首席秘书大工位 | 🎨 | 见 prompt A5.5 |
| 饮水机/绿植 | 🎨 | 可选装饰，后期添加 |

### 5.3 角色 Sprite

| 素材 | 方式 | 帧数 | 尺寸 | 说明 |
|------|------|------|------|------|
| L1 首席秘书（常驻） | 🎨 | 4帧 idle + 4帧 working | 32×32 | 始终在线，秘书造型，idle + 打字 |
| L2 Team Leader | 🎨 | 8帧 | 24×24 | walk ×2 + idle ×2 + working ×2 + error ×2 |
| L3 Agent 员工 | 🎨 | 8帧 | 24×24 | walk ×2 + idle ×2 + working ×2 + error ×2 |
| 女性/男性变体 | 🎨 | 各一套完整 | 同上 | L1/L2/L3 均需男女两版 |
| L1 秘书大工位 | 🎨 | 静态 + 屏幕亮起帧 | 96×64 | 比普通工位更大、更精致的秘书桌 |

**占位方案**：先用纯色矩形 + emoji 替代，后续替换 spritesheet。

**动画帧统一要求**：
- walk 帧：2帧走路循环（左右脚交替）
- idle 帧：2帧呼吸浮动
- working 帧：2帧打字（手指交替 + 屏幕闪烁）
- error 帧：2帧冒烟/着火

---

## 五-A、LLM 图片生成 Prompt 库

> 所有 prompt 均针对 AI 图片生成模型（Midjourney / DALL·E 3 / Stable Diffusion）优化。
> 统一风格要求：**8-bit pixel art, Katana Zero aesthetic, dark cyberpunk office interior**
> 所有 Sprite 要求透明背景，严格像素对齐，禁止抗锯齿。
> 生成后建议用 Aseprite / Photoshop 清理杂色并确认尺寸。

### A1. 办公楼外观（Title Scene 背景）

**用途**：标题画面主视觉，暗夜中的办公楼

```
A pixel art building exterior at night, 8-bit retro game style. 
A 6-story modern office building viewed from the front, dark purple-black sky background. 
Windows with warm amber/yellow light (#e8b450) glowing from inside, some windows dark. 
A neon sign on the building facade reading "SOLOQUEUE" in cyan (#50e0c0) pixel font with glow effect.
Sidewalk at the base, a single streetlamp casting warm light. 
Dark cyberpunk noir atmosphere like Katana Zero.
Strict pixel art, no anti-aliasing, clean pixel grid, 16-bit era style.
Resolution: 960×640 pixels. Dark moody palette: #0f0f1a background, #1a1a30 shadows, #e8b450 warm lights, #50e0c0 neon cyan.
--ar 3:2
```

### A2. L1 首席秘书角色 Sprite Sheet

**用途**：始终在线的主角色，专属大工位，用户的对话入口

```
A pixel art character sprite sheet for a chief secretary / personal assistant, 8-bit retro game style.
Front-facing view (top-down RPG perspective, looking slightly down at player).
Professional office attire — smart blazer or cardigan, neat appearance, approachable and competent.
Larger and more detailed than regular workers (32×32 pixels per frame).
Sprite sheet layout: 8 frames in a single row (256×32 total).
- Frame 1-2: idle (standing at attention, subtle breathing, slight sway)
- Frame 3-4: working (typing efficiently, head occasionally glancing at papers/screen)
- Frame 5-6: attentive (looking up toward the player/user, slight nod, as if acknowledging a request)
- Frame 7-8: thinking (hand on chin, pensive pose, then slight nod)
No walking frames needed (always at desk).
No background (transparent), strict pixel grid, no anti-aliasing.
Color palette: blazer in #3d3a58, shirt/blouse in #e0d8c8, subtle cyan accessory (#50e0c0 hair clip or tie), skin in warm beige.
Style reference: Katana Zero NPC sprites, clean pixel art with strong silhouettes.
Generate BOTH a male and female version as separate outputs.
```

### A3. L2 Team Leader 角色 Sprite Sheet

**用途**：各团队 Leader，从入口走入→坐到工位→工作→离开

```
A pixel art character sprite sheet for a team leader/manager, 8-bit retro game style.
Front-facing view (top-down RPG perspective).
Wears business casual — dark shirt with rolled sleeves, glasses.
Slightly more authoritative stance than regular workers, carries a clipboard or tablet.
Sprite sheet layout: 8 frames in a single row (192×24 total).
- Frame 1-2: walk cycle (walking toward desk, legs alternating, slight arm swing)
- Frame 3-4: idle (standing still at desk, subtle breathing, glancing at clipboard)
- Frame 5-6: working (typing on keyboard, head bobbing, screen glow on face)
- Frame 7-8: error/frustrated (rubbing temples, slight smoke puff)
Each frame is exactly 24×24 pixels, total image 192×24 pixels.
No background (transparent), strict pixel grid, no anti-aliasing.
Color palette: shirt in #3d3a58, pants in #1a1a30, glasses rim in #50e0c0, clipboard in #4a4a68, skin in warm beige.
Style reference: Katana Zero NPC sprites.
Generate BOTH a male and female version as separate outputs.
```

### A4. L3 Agent 角色 Sprite Sheet

**用途**：普通员工，从入口走入→坐到工位→执行任务→离开

```
A pixel art character sprite sheet for an office worker, 8-bit retro game style.
Front-facing view (top-down RPG perspective).
Casual office attire — dark hoodie or simple shirt, jeans.
Generic employee look, multiple variations possible (same base with different hair/colors).
Sprite sheet layout: 8 frames in a single row (192×24 pixels total).
- Frame 1-2: walk cycle (brisk walking, legs alternating, slight arm swing, heading toward desk)
- Frame 3-4: idle (standing at desk, subtle breathing, slight sway)
- Frame 5-6: working (typing rapidly on keyboard, head bobbing, screen glow in cyan #50e0c0 reflecting on face)
- Frame 7-8: error/panic (small smoke puffs above head, arms raised slightly, frantic expression)
Each frame is exactly 24×24 pixels, total image 192×24 pixels.
No background (transparent), strict pixel grid, no anti-aliasing.
Color palette: clothing in #2a2845 or #3d3a58, skin in warm beige, shoes in #1a1a30, screen glow in #50e0c0.
Style reference: Katana Zero background characters, simple but expressive pixel art.
Generate BOTH a male and female version as separate outputs.
```

### A5. 电脑显示器 + 办公桌（工位 Tileset）

**用途**：每个工位的核心家具，程序化搭建工位的基础素材

```
A pixel art tileset for an office cubicle desk setup, 8-bit retro game style.
Top-down or slightly isometric RPG perspective (looking down at ~45 degree angle).
Individual elements that can be composed into a full desk:
1. Office desk — dark wood/metal rectangular desk, warm brown-gray, 48×32 pixels
2. CRT computer monitor — bulky retro monitor with a glowing screen (cyan #50e0c0 when active, dark #1a1a30 when off), 32×24 pixels
3. Keyboard — small rectangular keyboard on desk surface, 16×8 pixels
4. Office chair — simple rolling chair from side/top angle, dark gray, 16×24 pixels
All elements on transparent background, neatly arranged in a single sprite sheet image.
Strict pixel grid, no anti-aliasing, clean pixel art.
Total tileset size: 160×128 pixels (arranged with spacing).
Color palette: desk in #3d3a58/#4a4a68, monitor body in #2a2845, screen in #50e0c0, chair in #1a1a30/#4a4a68.
Style reference: Katana Zero interior props.
```

### A5.5. L1 首席秘书专属工位

**用途**：L1 秘书的大型专属工位，比普通工位更精致显眼

```
A pixel art executive secretary desk setup, 8-bit retro game style.
Top-down RPG perspective (looking down at ~45 degrees).
A larger, more premium L-shaped wooden desk with dual monitors.
Items on the desk:
- Two CRT monitors side by side, both with cyan glow (#50e0c0) indicating active status
- A small nameplate reading "SECRETARY" or a golden star emblem
- A coffee mug with subtle steam wisps
- A neat stack of documents/papers in a tray
- A small desk lamp casting warm amber light (#e8b450) in a small radius
- A potted tiny plant in the corner for decoration
The chair is slightly larger and more ergonomic than regular office chairs.
Resolution: 96×64 pixels. No background (transparent).
Strict pixel grid, no anti-aliasing.
Color palette: desk in #3d3a58 with #4a4a68 edges, monitors in #2a2845, screens in #50e0c0,
lamp glow in #e8b450 (warm), nameplate in #e0d8c8, plant in muted green.
Style reference: Katana Zero boss room interiors, but warmer and more approachable.
```

### A13. 办公楼入口 / 电梯 Spawn 点

**用途**：Agent spwan/despawn 的视觉锚点

```
A pixel art office building entrance / elevator lobby, 8-bit retro game style.
Front-facing view for a 2D side-scrolling office layout.
A pair of elevator doors or a main office entrance door:
- Two metal sliding doors (elevator style) or one grand entrance double door
- A small overhead sign reading "ENTRANCE" in pixel font
- A floor indicator above the door showing an arrow or level number
- Subtle light glow from the door gap when opening (cyan #50e0c0 or warm #e8b450)
- A small welcome mat or lobby floor tile pattern in front
Resolution: 64×96 pixels. No background (transparent).
Strict pixel grid, no anti-aliasing.
Color palette: door frame in #2a2845, door panels in #3d3a58, sign in #50e0c0 glow, floor mat in #4a4a68.
Include an "open" variant frame (doors slightly apart with glow inside) as second row.
Total image: 64×192 (two rows: closed + open).
Style reference: Katana Zero level entrance doors with neon signage.
```

### A6. 办公室门与隔断墙

**用途**：划分各团队区域的门和墙体

```
A pixel art tileset for office interior walls and doors, 8-bit retro game style.
Side-view perspective for a 2D top-down office layout. Elements include:
1. Cubicle partition wall — half-height office divider, gray-tan fabric texture, 64×48 pixels vertical
2. Door (closed) — wooden office door with a small window and handle, warm brown, 24×48 pixels
3. Door (open) — same door but open showing dark interior, 48×48 pixels
4. Wall section — solid interior wall with subtle texture, dark purple-gray (#2a2845), 32×48 pixels
5. Glass wall section — same wall but with transparent window panel showing cyan-tinted glass, 32×48 pixels
All elements on transparent background, sprite sheet layout.
Strict pixel grid, no anti-aliasing, clean pixel art.
Color palette: walls in #2a2845/#1a1a30, door in #3d3a58 with #e8b450 handle, glass in semi-transparent #50e0c0.
Total image size: 256×128 pixels.
Style reference: Katana Zero level architecture.
```

### A7. 看板墙（Kanban Board）

**用途**：挂在每个团队区域墙壁上的看板

```
A pixel art wall-mounted kanban/task board, 8-bit retro game style.
A large cork board or whiteboard mounted on an office wall, viewed from the front.
Contains:
- Board frame: dark wood or metal border (#3d3a58)
- Board surface: cork texture in warm brown or whiteboard in off-white
- Several small colorful sticky notes/kanban cards pinned to it:
  - Some cyan (#50e0c0) — in progress
  - Some orange (#f08040) — blocked/waiting
  - Some green (#50e0c0) — done
  - Some pink (#e05060) — urgent/error
- Column headers in pixel font: "TODO", "DOING", "DONE"
- Small pushpins in metallic gray
Resolution: 128×96 pixels. No background (transparent).
Strict pixel grid, no anti-aliasing.
Style reference: Papers Please board aesthetic merged with Katana Zero interior props.
```

### A8. 霓虹招牌 "TEAM" 装饰

**用途**：每个团队区域上方的霓虹标识

```
A pixel art neon sign reading "TEAM A", 8-bit retro cyberpunk style.
Wall-mounted neon tube sign with glowing effect.
The text "TEAM" in large pixel letters with cyan neon glow (#50e0c0).
A small blinking effect suggestion — subtle glow radius around the tubes.
Mounted on a dark metal bracket against a #1a1a30 wall background.
Resolution: 128×32 pixels. No background (transparent wall area — will be placed over wall sprite).
Strict pixel grid for the letters, glow can have slight transparency.
Color: tube in pure white #ffffff center, glow in #50e0c0 with decreasing opacity.
Style reference: Katana Zero neon signs, cyberpunk bar aesthetic.
Note: generate one version, the "A" will be programmatically swapped per team.
```

### A9. 首席秘书办公桌（豪华工位）

**用途**：L1 秘书使用的大型办公桌，带双屏

```
NOTE: This prompt has been superseded by A5.5. Use A5.5 instead.
```

### A10. 饮水机（可选装饰）

**用途**：茶水间/走廊装饰物

```
A pixel art office water cooler/dispenser, 8-bit retro game style.
Side-view. A tall standing water dispenser unit — white/cream body with a blue water jug on top.
Simple design: rectangular base, cylindrical water jug with visible water level line.
Two small spigots (red for hot, blue for cold) at the front.
Paper cup dispenser mounted on the side.
Resolution: 16×32 pixels. No background (transparent).
Strict pixel grid, no anti-aliasing.
Color palette: body in off-white #e0d8c8, water jug in semi-transparent light blue, spigots in #e05060 and #50e0c0.
```

### A11. 绿植盆栽（可选装饰）

**用途**：办公室装饰，增加场景生动感

```
A pixel art potted office plant, 8-bit retro game style.
Side-view. A medium-sized indoor plant in a simple dark pot.
Several broad green leaves extending upward and outward from a cylindrical dark gray pot.
Simple silhouette, 2-3 shades of green for leaves.
Resolution: 16×24 pixels. No background (transparent).
Strict pixel grid, no anti-aliasing.
Color palette: pot in #1a1a30, leaves in 3 shades of muted green, soil in #3d3a58.
```

### A12. 状态特效叠加（Sprite 叠加层）

**用途**：覆盖在 Agent Sprite 上表现特殊状态

```
A pixel art status effect overlay spritesheet, 8-bit retro game style.
Individual animated effects that render ON TOP of character sprites:
1. Working particles — tiny cyan squares (#50e0c0) floating upward from keyboard area, 4 frames, each 6×6
2. Error smoke — gray/black smoke puffs rising from character head, 4 frames, each 16×16
3. Idle "..." bubble — a small thought bubble with three dots cycling, 4 frames, each 20×16
4. Glow/aura ring — a subtle pulsing ring around the character feet (indicating active processing), cyan #50e0c0, 4 frames, each 32×8
All frames in a single sprite sheet image, each effect in its own row.
Transparent background, strict pixel grid, no anti-aliasing.
Total image size: 128×64 pixels.
```

### A14. 角色创建预览 Sprite（男/女，正面立绘）

**用途**：Character Creation Scene 中预览 L1 秘书角色

```
A pixel art character portrait / full-body preview, 8-bit retro game style.
Front-facing full-body view (standing pose), for character creation screen preview.
Two separate images:
1. MALE version: smart business casual — dark blazer, neat shirt, professional appearance
2. FEMALE version: smart business casual — fitted blazer or cardigan, professional appearance
Both should be:
- Standing upright, hands at sides or one hand slightly raised in greeting
- Friendly but professional expression
- 48×48 pixels per character (larger than in-game for detail viewing)
- Neutral background (transparent)
- Strict pixel grid, no anti-aliasing, clean pixel art
Color palette: blazer in #3d3a58, shirt in #e0d8c8, hair in dark brown or black, skin in warm beige.
Style reference: Katana Zero NPC dialogue portraits, RPG character status screen art.
Generate BOTH the male and female version, clearly labeled.
```

---

### Prompt 使用说明

1. **生成顺序**：A4（L3 Agent）→ A3（L2 Leader）→ A2（L1 秘书）→ A14（预览立绘）→ A5（工位）→ A5.5（L1 秘书桌）→ A13（入口）→ A1（办公楼）→ A6-A8 → A9 → A12 → A10-A11
2. **性别变体**：A2/A3/A4/A14 均需生成男性版和女性版，共 8 套角色素材
3. **尺寸验证**：生成后用图片工具确认像素尺寸是否精确，超出的裁剪，不足的补像素
4. **调色板统一**：所有素材拖入 Aseprite 等像素编辑工具，统一应用项目色板（见 3.2）
5. **后期处理**：AI 生成的像素图通常会有杂色/半透明像素，需要手动清理成纯色像素
6. **背景透明**：生成后需用工具去除背景（remove.bg 或手动抠图），确保素材叠放无白边
7. **多版本**：每个 prompt 建议生成 3-4 个变体，挑选最佳的一个进行清理使用

### 5.4 UI 组件（全部 CSS/Canvas 实现）

| 组件 | 方式 | 说明 |
|------|------|------|
| 像素按钮（开始/返回/设置） | 🖥️ | CSS border + box-shadow 模拟像素边框，`image-rendering: pixelated` |
| 像素文字 | 📝 | Press Start 2P 字体（英文）+ Zpix 或 similar（中文） |
| 像素边框面板 | 🖥️ | CSS 粗边框 (`4px solid`) + 内角锯齿效果 |
| 像素下拉选择器 | 🖥️ | CSS 自定义 select，三角箭头用 border 绘制 |
| 像素进度条 | 🖥️ | CSS div 分段填充，每段 4px |
| 状态指示灯 | 🖥️ + 🎮 | CSS 圆点 + 脉冲动画 / Canvas 呼吸灯 |
| 对话框（像素边框） | 🖥️ | CSS 绝对定位，像素边框 + 暗色半透背景 |
| 像素 icon（关闭/返回/设置） | 🖥️ | CSS 绘制 8×8 或 16×16 像素 icon |
| 滚动条 | 🖥️ | CSS 自定义像素滚动条 |
| 数字/统计显示 | 📝 | 像素字体大号数字 |
| 通知气泡 | 🖥️ | CSS 像素气泡 + 闪烁提示 |

### 5.5 统计数据

| 需求 | 素材制作方式 |
|------|-------------|
| 角色 Sprite（L1/L2/L3，各男女两版） | 🎨 需要，共 6 套 spritesheet |
| 角色创建预览立绘（男女） | 🎨 需要，见 A14 |
| 办公楼外观 | 🎨 建议有，初期 CSS 搭建 |
| 办公楼入口/电梯 | 🎨 需要，见 A13 |
| L1 秘书大工位 | 🎨 需要，见 A5.5 |
| 普通工位 tileset | 🎨 需要，见 A5 |
| 像素字体 | 📝 免费商用（Press Start 2P, Zpix） |
| 音效（可选） | 🎵 后期添加，使用 bfxr/jsfxr 生成 |

**总结**：核心需要 AI 生成的像素素材约 **9 套**（A1-A14 中有明确 🎨 标记的）。其余墙壁/地板/走廊/粒子效果等均可 Canvas 程序化绘制。

---

## 六、系统映射

### 6.1 游戏概念 ↔ soloQueue 实体

| 游戏层 | soloQueue 实体 | 视觉表现 |
|--------|---------------|---------|
| 公司 | 整个多 Agent 系统 | 办公楼 |
| 首席秘书 | L1 Agent（常驻） | 右下角专属大工位，始终在线 |
| 部门 | Team（含 L2 Leader） | 隔间办公区 + 霓虹招牌 |
| 临时员工 | L3 Agent（按需调度） | 从入口走入 → 坐到工位 → 完成离场 |
| 临时主管 | L2 Agent（按需调度） | 同上，体型/服饰区别于 L3 |
| 部门看板 | Kanban（按 team 分列） | 墙壁上的看板 |
| 公司资产 | Token 消耗 | 顶部 HUD 数字 |
| 调度队列 | 即将被调度的任务 | 入口处等候区显示 |

### 6.2 Agent 状态 ↔ 视觉表现

| Agent 状态 | 触发条件 | 动画 | 位置 |
|-----------|---------|------|------|
| unsummoned | 无任务，不在场景中 | 不存在于画布 | — |
| walking_in | 被调度，spawn 后走向工位 | walking 动画，沿走廊移动 | 入口 → 工位路径 |
| sitting_down | 到达工位，准备开始 | 坐下过渡动画（0.5s） | 工位旁 |
| working | 正在执行 LLM 调用 | 快速敲键盘，屏幕霓虹青闪烁，粒子飞出 | 坐在工位 |
| tool_calling | 执行工具 | 手上工具图标切换，屏幕快速闪 | 坐在工位 |
| waiting | 等待用户输入/确认 | 转头看门口，手指轻敲桌面 | 坐在工位 |
| error | LLM 返回错误 | 冒烟/着火 | 坐在工位 |
| walking_out | 任务完成/超时 | walking 动画，向入口移动 | 工位 → 入口路径 |
| despawned | 到达入口消失 | 消失粒子效果 | — |

**状态颜色对应**：
- working：霓虹青呼吸光（#50e0c0）
- waiting：霓虹橙闪烁（#f08040）
- error：霓虹品红脉冲（#e05060）
- walking：无特殊光色，正常行走
- L1（常驻）：idle 时微小浮动，始终在工位

### 6.3 Kanban 改造

现有 Kanban 是全局四列（Backlog → Todo → Running → Done），需加入团队维度：

```
┌────────── Team A 看板 ──────────┐
│ Backlog │ Todo │ Running │ Done │
│ ┌────┐  │┌────┐│┌──────┐ │┌───┐│
│ │ 📋 │  ││ 📋 │││ 📋   │ ││ ✓ ││
│ │[A]  │  ││[A] │││[A]   │ ││[A]││  ← 卡片标注团队
│ └────┘  │└────┘││ 👤A1  │ │└───┘│
│          │      │└──────┘ │     │
│ ┌────┐  │      │┌──────┐ │     │
│ │ 📋 │  │      ││ 📋   │ │     │
│ │[A]  │  │      ││[A]   │ │     │
│ └────┘  │      ││ 👤A2  │ │     │
│          │      │└──────┘ │     │
└─────────────────────────────────┘
```

卡片字段新增 `team` 属性，看板支持：
- 按团队筛选（顶部 Tab）
- 全局视图（所有团队混排，用颜色/标签区分）

---

## 七、目录结构

```
desktop/
├── DESIGN.md                    # 本文档
├── package.json                 # Electron + React 依赖
├── electron.vite.config.ts     # electron-vite 配置
├── tsconfig.json
├── tsconfig.node.json
├── tsconfig.web.json
├── tailwind.config.ts           # Tailwind v4
├── postcss.config.js
├── pnpm-lock.yaml
├── resources/                   # Electron 打包资源
│   └── icon.png
├── src/
│   ├── main/                    # Electron 主进程
│   │   ├── index.ts             # 入口：创建窗口、spawn Go 后端
│   │   └── backend.ts           # Go 子进程管理（启动/重启/健康检查）
│   ├── preload/                 # 预加载脚本
│   │   └── index.ts             # contextBridge 暴露 IPC API
│   ├── renderer/                # React 像素游戏壳
│   │   ├── index.html
│   │   ├── main.tsx             # React 入口
│   │   ├── App.tsx              # 场景路由
│   │   ├── styles/
│   │   │   ├── index.css        # Tailwind + 像素全局样式
│   │   │   ├── fonts.css        # 像素字体加载
│   │   │   └── pixel-ui.css     # 像素 UI 组件样式
│   │   ├── scenes/
│   │   │   ├── TitleScene.tsx         # 标题画面（办公楼 + 开始按钮）
│   │   │   ├── CharacterCreateScene.tsx # 角色创建向导（首次启动）
│   │   │   ├── OfficeScene.tsx        # 办公层主场景（Konva Canvas）
│   │   │   └── KanbanScene.tsx        # 看板全屏视图
│   │   ├── components/
│   │   │   ├── PixelButton.tsx
│   │   │   ├── PixelPanel.tsx
│   │   │   ├── PixelDialog.tsx
│   │   │   ├── PixelProgress.tsx
│   │   │   ├── PixelHUD.tsx           # 顶部/底部状态栏
│   │   │   ├── AgentSprite.tsx        # Agent 像素小人（Konva.Group）
│   │   │   ├── DeskUnit.tsx           # 工位单元（桌子+椅子+电脑，空/使用中）
│   │   │   ├── EntranceUnit.tsx       # 办公楼入口（电梯 spawn 点）
│   │   │   ├── TeamRoom.tsx           # 团队办公区（隔间+空工位+看板）
│   │   │   ├── L1Desk.tsx             # 首席秘书专属工位
│   │   │   ├── KanbanBoard.tsx        # 像素看板组件
│   │   │   ├── KanbanCard.tsx         # 像素卡片
│   │   │   ├── AgentDetailSheet.tsx   # Agent 详情侧边栏
│   │   │   ├── QueuePanel.tsx         # 入口调度队列面板
│   │   │   └── CharacterPreview.tsx   # 创建角色时的像素预览
│   │   ├── systems/
│   │   │   ├── agentManager.ts        # Agent 状态机（unsummoned→walking→working→...）
│   │   │   ├── teamManager.ts         # Team 分组逻辑
│   │   │   ├── pathfinding.ts         # A* 寻路 + 栅格化 walkable grid
│   │   │   └── gameLoop.ts            # 60fps 游戏循环（动画 + 寻路步进）
│   │   ├── stores/
│   │   │   ├── gameStore.ts     # 游戏状态（当前场景、相机位置）
│   │   │   ├── agentStore.ts    # Agent 数据（复用 web）
│   │   │   ├── teamStore.ts     # Team 数据
│   │   │   └── kanbanStore.ts   # Kanban 数据
│   │   ├── hooks/
│   │   │   ├── useAgents.ts     # Agent WebSocket hook
│   │   │   ├── useTeams.ts
│   │   │   ├── useKanban.ts
│   │   │   └── useGameLoop.ts   # 游戏循环 hook
│   │   ├── lib/
│   │   │   ├── api.ts           # REST API（复用 web）
│   │   │   ├── websocket.ts     # WS 管理（复用 web）
│   │   │   └── sprite.ts        # Sprite 加载/缓存工具
│   │   └── types/
│   │       └── index.ts         # 类型定义
│   └── shared/                  # 主进程 ↔ 渲染进程共享
│       └── ipc.ts              # IPC channel 定义
└── build/                       # electron-builder 输出
```

---

## 八、场景切换流程

```
                       首次启动 (无 soul)
                            │
                            ↓
                    ┌────────────────┐
                    │  Character     │   Step 1→2→3
                    │  Creation      │  性别→名字→风格
                    │  Scene         │
                    └───────┬────────┘
                            │ 确认入职
                            ↓
┌──────────┐  点击 START   ┌──────────┐  点击看板    ┌──────────┐
│  Title   │ ────────────→ │  Office  │ ──────────→ │  Kanban  │
│  Scene   │ ←──────────── │  Scene   │ ←────────── │  Scene   │
└──────────┘   按 ESC      └──────────┘   按 ESC     └──────────┘
                   │              │
                   │ 点击 Agent    │ 点击入口
                   ↓              ↓
            ┌────────────┐  ┌────────────┐
            │  Detail    │  │  Queue     │
            │  Sidebar   │  │  View      │
            │  (Drawer)  │  │  (Drawer)  │
            └────────────┘  └────────────┘
```

- Character Creation Scene：纯 React DOM（像素 UI 面板 + 人物预览）
- Title Scene：纯 React DOM（HTML/CSS）
- Office Scene：Konva Canvas（一个 Stage 渲染所有办公层内容）
- Kanban Scene：React DOM（看板本质是列表式 UI，React + CSS 更合适）
- Detail Sidebar：React DOM Drawer 覆盖在 Canvas 上方

---

## 九、实现阶段

### Phase 1：Electron 基建 + 像素主题系统（Week 1-2）
- [ ] electron-vite 项目骨架搭建
- [ ] Go 后端子进程 spawn/管理
- [ ] 像素 UI 组件库（PixelButton, PixelPanel, PixelDialog 等）
- [ ] 像素字体加载
- [ ] 暗色主题 Tailwind 配置
- [ ] WebSocket + REST 通信层复用

### Phase 2：标题画面 + 角色创建（Week 2）
- [ ] TitleScene：办公楼外观 + 开始按钮 + 粒子背景
- [ ] Character Creation Scene：三步向导 + 人物预览 + soul 写入
- [ ] 首次启动检测逻辑（检查 soul/profile 是否存在）

### Phase 3：办公层场景 + 寻路系统（Week 3-4）
- [ ] OfficeScene：Konva Stage + Layer 结构搭建
- [ ] 地板/墙体/走廊程序化渲染
- [ ] 入口 spawn 点 + L1 秘书工位 + Team 隔间布局
- [ ] 空工位组件（桌+椅+熄屏显示器）
- [ ] 栅格化 A\* 寻路系统 + 走廊 walkable grid 标记
- [ ] Agent 生命周期：spawn → walk → sit → work → walk → despawn
- [ ] AgentSprite 占位（矩形+emoji）+ 行走/工作动画

### Phase 4：交互 + Kanban（Week 4-5）
- [ ] 点击工作中的 Agent → 侧边栏详情
- [ ] 点击看板 → KanbanScene
- [ ] 点击入口 → 调度队列预览
- [ ] Kanban 按团队分组展示
- [ ] Agent 实时状态更新（WebSocket → 状态机 → 动画切换）
- [ ] 相机移动/缩放

### Phase 5：打磨 + 素材替换（Week 5+）
- [ ] 替换占位 Sprite 为像素美术
- [ ] 音效集成（可选）
- [ ] electron-builder 打包配置
- [ ] 应用图标
- [ ] 窗口管理（最小化到托盘、开机启动等）

---

## 十、Game Loop 设计

```
┌─────────────────────────────────────────────────────┐
│              requestAnimationFrame                   │
│                                                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────┐  │
│  │ WS Pull  │  │Pathfinder│  │ Animate  │  │Rend│  │
│  │ (缓存)   │  │  A* Tick │  │ Tweens   │  │er  │  │
│  └──────────┘  └──────────┘  └──────────┘  └────┘  │
│                                                     │
│  每帧执行：                                          │
│  1. 检查 WS 消息缓存，更新 Agent 状态到 store        │
│  2. 对每个 walking 的 Agent 执行 A* 路径步进         │
│  3. 插值所有动画 tween（位置、颜色、透明度）          │
│  4. Konva layer.batchDraw() 渲染                  │
└─────────────────────────────────────────────────────┘
```

**Pathfinding 细节**：
- 场景划分 16×16 逻辑网格（基于工位和走廊宽度）
- 每个 walkable 格子存储 x,y 坐标（Konva 世界坐标）
- A\* 算法每帧步进 1 格（Agent 速度 ≈ 64px/s = 4 格/s ≈ 每 4 帧移动 1 格）
- L2/L3 Agent spawn 后从入口网格出发，计算到目标工位的最短路径
- 离开时计算从工位到入口的反向路径
- 路径预计算并缓存（障碍物布局不变，只需计算一次到各工位的路径）
- 多 Agent 同时移动时，简单处理：不碰撞，可以重叠通过（像素风不必太真实）

- WebSocket 消息频率低（按秒级），不放在 frame loop 里等待
- Frame loop 只负责动画插值 + 寻路步进 + Konva 渲染
- 使用 `requestAnimationFrame` 驱动，非活跃窗口自动降帧

---

## 十一、关键设计决策记录

1. **只用一层楼**：所有 Team 在同一个平面布局，用隔断墙分隔，不是多层建筑
2. **L2/L3 按需调度**：Agent 闲置时不存在于场景中。收到任务时 spawn 在入口，walk 到工位；完成后 walk 回入口 despawn。工位默认是空的
3. **L1 首席秘书**：L1 是始终在线的首席秘书角色，有专属大型工位（右下角），是用户的直接对话入口
4. **首次创建角色**：无 soul/profile 时进入创建向导（性别→名称→风格），创建 L1 秘书人设
5. **A\* 寻路**：栅格化 16×16 网格，走廊标记为 walkable，路径预计算缓存。Agent 速度 64px/s
6. **无碰撞**：多人同时移动时不碰撞，可以重叠通过（像素风不必太真实，简化实现）
7. **Canvas（Konva）vs DOM**：办公场景用 Konva Canvas（像素 Sprite 渲染 + 寻路），UI 面板用 React DOM（表单/Accessibility 好）
8. **像素占位方案**：先用 `Konva.Rect` + 纯色绘制所有元素，等像素素材就绪后替换为 `Konva.Image`
9. **Kanban 改造**：卡片加入 `teamId` 字段，看板支持全局视图 / 按团队筛选
