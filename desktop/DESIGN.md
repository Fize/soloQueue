# SoloQueue Desktop — 像素模拟经营系统设计文档

## 一、项目概述

将 soloQueue 的 Web UI 重构为 Electron 桌面应用，将 soloQueue 的 Web UI 重构为 Electron 桌面应用，外壳采用 **8-bit 像素风模拟经营游戏** 风格。视觉参考 **“星露谷暖原木像素办公风” (Stardew Cozy Wood Office Style)**，融合原木色调与现代办公室经营的玩法。用户启动后看到朝阳照耀下的温馨现代办公楼外观 → 点击开始 → 进入温暖橡木与春绿地毯点缀的办公层内部，以游戏化方式管理和监控多 Agent 系统。

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

### 3.1 参考风格：星露谷暖原木像素办公风（Stardew Cozy Wood Office Style）

融合了《星露谷物语》的温暖原木与舒适配色，同时保持现代办公楼和办公室的主题场景。整体色调以温暖橡木黄、作物翠绿（运行）、浆果深红（错误）为主，抛弃冷酷暗黑的赛博朋克基调，营造温暖、舒适且富有掌控感的现代像素办公室氛围：

- **温暖舒缓**：底色以温馨的仿复古木纹/羊皮纸色为主，大大降低长久监控的眼部疲劳，带来温馨舒适的办公体验。
- **舒适办公**：办公室隔断使用松针绿或暖木色，办公家具采用沉稳的橡木/枫木黄褐色，保留完整的现代办公室格局与工位，让玩家在温馨舒适的氛围中管理团队。
- **色彩高亮**：交互和状态反馈融入大自然作物与经典模拟经营色彩（作物绿表示运行，浆果红表示报错，南瓜黄表示阻塞）。
- **像素质感**：颗粒感强，粗木纹边框，UI 面板采用木质公告栏或经典羊皮纸面板设计。

### 3.2 配色方案

```
┌──────────┬──────────┬──────────────────────────┐
│ 角色     │ 色值     │ 用途                     │
├──────────┼──────────┼──────────────────────────┤
│ 羊皮纸黄 │ #f6ebd3  │ 全局大背景、文本面板底色 │
│ 浅驼色   │ #e3d3b4  │ 未激活区域、标签页暗面   │
│ 春绿地毯 │ #7ca84c  │ 办公室走道地毯、公共区   │
│ 橡木中调 │ #b86a34  │ 办公桌、家具、面板背景   │
│ 太阳金黄 │ #f3b72b  │ 吊灯、台灯、金星交互高亮 │
│ 作物翠绿 │ #4eb036  │ Agent 运行中、成功状态   │
│ 南瓜橘黄 │ #e28a2b  │ 警告、超时、阻塞         │
│ 浆果深红 │ #d83838  │ 错误、异常、宕机         │
│ 木炭深褐 │ #381a04  │ 主文字（仿黑巧克力色）   │
│ 迷雾灰褐 │ #8c7662  │ 次要文字、不可用项       │
│ 红松木框 │ #5a2800  │ 像素面板边框、隔断墙边   │
│ 暖秋遮罩 │ rgba(90,40,0,0.45) │ 暖棕色弹窗遮罩  │
└──────────┴──────────┴──────────────────────────┘
```

### 3.3 像素比例

- 画面基础像素单位：4px（屏幕 1 像素 = 1/4 游戏像素，放大 4 倍渲染）
- 所有 UI 元素对齐 4px 网格
- 文字使用像素字体，字号必须是 4 的倍数

---

## 四、场景设计

本系统完全采用 **HTML5 Canvas (通过 react-konva 库)** 实现像素模拟经营场景渲染，所有场景交互、寻路及动画均在 Canvas 树中按帧更新。以下是首页（Title Scene）与主场景（Main Office Scene）的核心渲染架构与组件树设计：

### 4.1 标题画面（Title Scene）

#### 4.1.1 渲染结构与 Konva 节点树
标题画面负责展现现代办公楼的外观（采用星露谷暖木色调）、动态天气粒子效果以及复古木质 UI 选项：
```
<Stage width={960} height={640}>
  {/* Layer 0: 天空与天气环境层 */}
  <Layer id="sky-and-weather">
    <Rect width={960} height={640} fillLinearGradientStartPoint={{ x: 0, y: 0 }} fillLinearGradientEndPoint={{ x: 0, y: 640 }} fillLinearGradientColorStops={[0, '#b0d0f0', 1, '#f6ebd3']} />
    <Group id="clouds" />  {/* 程序化绘制的浮动像素白云 */}
    <Group id="rain-particles" /> {/* Canvas 粒子：下落的像素雨丝线 + 水花溅射圆圈 */}
  </Layer>

  {/* Layer 1: 办公楼实体层 */}
  <Layer id="scenic-building">
    <Image image={buildingImage} x={160} y={80} width={640} height={420} /> {/* 暖木色调现代办公楼 PNG */}
    <Group id="window-lights" /> {/* 窗口暖黄色发光：基于 Canvas 脉冲动画，部分窗口明暗交替 */}
  </Layer>

  {/* Layer 2: UI 标题与选项覆盖层 */}
  <Layer id="ui-overlay">
    {/* 太阳金黄发光标题 */}
    <Text text="SOLOQUEUE INC." fontSize={48} fontFamily="PixelFont" fill="#f3b72b" stroke="#5a2800" strokeWidth={4} align="center" x={0} y={150} width={960} />
    <Text text="Est. 2026" fontSize={16} fontFamily="PixelFont" fill="#8c7662" align="center" x={0} y={210} width={960} />

    {/* 选项菜单：仿羊皮纸材质木条框按钮 */}
    <Group id="menu-buttons" x={380} y={350}>
      <PixelButton text="START" y={0} onClick={handleStart} />
      <PixelButton text="Settings" y={50} onClick={handleSettings} />
    </Group>
    
    <Text text="v0.1.0" fontSize={12} fontFamily="PixelFont" fill="#8c7662" x={20} y={600} />
  </Layer>
</Stage>
```

#### 4.1.2 动态效果实现
- **雨水粒子**：粒子系统中每个雨滴是一个存储 `{x, y, vy, length}` 的对象，在每一帧中 `y += vy`，当 `y` 超过屏幕底部或大楼轮廓时，在该点触发一个短暂的像素水花溅射动画，随后重置雨滴。
- **浮动白云**：云层由几组预设的像素多边形块构成，在 x 轴上以微慢速度（如 `5px/s`）移动，移出屏幕后重新在最左侧生成。
- **窗口微光**：每个窗口节点绑定一个正弦淡入淡出（Sine opacity）插值器，使其发光强度随时间自然起伏。

### 4.1-A 角色创建画面（Character Creation Scene）— 首次启动

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
│          │   木雕招牌背光    │                │
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
- 木雕招牌 "SOLOQUEUE" → **CSS 文字 + text-shadow 柔和背光**
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

#### 4.2.1 渲染结构与 Konva 节点树
主场景使用支持多图层叠加的 Canvas 渲染树，以保证地面的连续性、家具和 Agent 之间的遮挡关系（Y-Sorting）以及氛围环境光效：
```
<Stage width={viewportWidth} height={viewportHeight} draggable>
  {/* Layer 0: 地面与铺设层 */}
  <Layer id="floor-layer">
    <Rect width={2000} height={1500} fillPatternImage={officeFloorPattern} /> {/* 办公室走廊铺设的绿织地毯 */}
    <Rect x={100} y={100} width={1800} height={1300} fillPatternImage={woodFloorPattern} /> {/* 办公室内橡木地板拼贴 */}
  </Layer>

  {/* Layer 1: 墙体与固定障碍结构层 */}
  <Layer id="walls-and-partitions">
    <Group id="outer-walls" />     {/* 办公室外围木框墙 */}
    <Group id="office-dividers" /> {/* 团队间的隔断屏风和玻璃墙 */}
  </Layer>

  {/* Layer 2: 动态排序深度层 (Y-Sorting Layer) */}
  {/* 关键：本层所有子节点（家具、工位、Agent、看板）在每帧运行后按其 Y 轴坐标重新排序 Z-Index */}
  <Layer id="depth-sorted-layer">
    {/* 场景家具：办公桌、办公椅、电脑显示器 */}
    <Group id="workstations">
      {/* 每个工位包含：桌子 Image、椅子 Sprite、显示器 Sprite (亮屏/熄屏状态) */}
      <Workstation x={200} y={300} deskId="desk-A1" />
      <Workstation x={350} y={300} deskId="desk-A2" />
    </Group>

    {/* 墙面挂载物：木质看板 */}
    <KanbanBoard x={250} y={120} teamId="team-A" />

    {/* 角色 Sprites: 首席秘书、Team Leader、Agent */}
    <AgentSprite id="agent-L1" type="L1" x={1500} y={1100} />
    {/* walking、working 状态 of the Agent 会根据当前坐标 Y 值与桌椅动态排序遮挡 */}
    {activeAgents.map(agent => (
      <AgentSprite key={agent.id} id={agent.id} type={agent.type} x={agent.x} y={agent.y} state={agent.state} />
    ))}
  </Layer>

  {/* Layer 3: 氛围环境与特效层 */}
  <Layer id="atmosphere-and-lights">
    {/* 吊灯、台灯的 Canvas 径向渐变，实现星露谷般的温暖光照效果 */}
    <Circle x={200} y={280} radius={120} fillRadialGradientStartRadius={0} fillRadialGradientEndRadius={120} fillRadialGradientColorStops={[0, 'rgba(243,183,43,0.3)', 1, 'rgba(243,183,43,0)']} globalCompositeOperation="screen" />
    <Group id="status-particles" /> {/* Agent 打字时漂浮的绿色像素方块，异常时的暗红冒烟粒子 */}
  </Layer>

  {/* Layer 4: HUD 悬浮层 (不随镜头平移/缩放) */}
  <Layer id="hud-overlay">
    <HudParchmentPanel x={20} y={20} /> {/* 羊皮纸信息牌：显示 Token、卡片数、在线 Agent 数 */}
    <QueuePanel x={viewportWidth - 240} y={20} /> {/* 入口调度队列 */}
  </Layer>
</Stage>
```

#### 4.2.2 遮挡关系处理 (Y-Sorting / Depth Sorting)
在 2D 俯视角模拟经营游戏中，当 Agent 走到办公桌后方时，身体应当被办公桌遮挡；而走到办公桌前方时，应当遮挡桌椅。
- **具体做法**：将所有的“办公桌”、“办公椅”、“小人”、“饮水机”等实体全部放到同一个 Konva Layer (`depth-sorted-layer`) 下作为直属子节点。
- **动态更新**：在 Game Loop 的每一帧或每当 Agent 坐标更新时，对该 Layer 的子节点执行一次基于 Y 轴坐标的排序：
  ```javascript
  const children = depthSortedLayer.getChildren();
  children.sort((a, b) => {
    // 取得实体的基准 Y 坐标（脚底接触地面的位置）
    const yA = a.getAttr('anchorY') || a.y();
    const yB = b.getAttr('anchorY') || b.y();
    return yA - yB;
  });
  // 按照排序后的顺序重新安排渲染层级
  children.forEach((child, index) => child.setZIndex(index));
  ```

#### 4.2.3 场景布局逻辑图
```
┌──────────────────────────────────────────────┐
│  ┌──────────────────────────────────────┐    │
│  │  SOLOQUEUE INC.    09:41 PM    ⚙   │    │  ← 顶部 HUD (HUD Layer)
│  └──────────────────────────────────────┘    │
│                                              │
│  ┌───────────────┐ ┌──────────┐ ┌────────┐  │
│  │   Team A      │ │ Team B   │ │ Team C │  │  ← 团队隔间 (Depth Layer)
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
│  │ L1 秘书工位│          │   🚪 入口  │  │  ← L1 常驻 + 入口 (Depth Layer)
│  │  ┌──────┐  │          │  ELEVATOR  │  │
│  │  │ 👩‍💼  │  │          │  员工进出   │  │
│  │  │ L1   │  │          │  ──→ ←── │  │
│  │  └──────┘  │          └────────────┘  │
│  └────────────┘                          │
│                                          │
│  ┌──────────────────────────────────────┐│
│  │ 💰 24,520  📋 18 tasks  👥 3 active ││  ← 底部 HUD (HUD Layer)
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
  - Team 名称木雕招牌
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

在开发 8-bit 星露谷风格的模拟经营系统时，虽然使用 HTML5 Canvas 技术，但对于**无法用纯前端代码实现、必须使用图片文件 (PNG/SVG)** 的素材，和**完全可以用前端代码动态生成/绘制**的素材，有清晰的划分标准。

### 5.1 必须使用图片文件 (PNG) 的外部素材
这些素材细节丰富、有精致的手绘像素纹理与分帧序列，**无法使用 Canvas 矢量路径高效绘制**，必须由美术绘制或 AI 生成为 PNG 图片加载：

1. **角色动作精灵图 (Agent Spritesheets - A2/A3/A4/A14)**:
   - 包括小人的走路、打字、异常、思索等分帧动作。必须加载为带透明通道的 PNG 精灵图，前端使用 `Konva.Sprite` 截取并播放对应帧。
2. **家具与工位拼图集 (Furniture Tileset - A5/A5.5)**:
   - 包含橡木办公桌、CRT 复古电脑显示器（亮屏/暗屏两帧）、木质办公椅、红瓦盆栽等。手绘像素质感的斜 45 度家具无法用代码精确勾勒，必须作为 PNG 图片加载并切割渲染。
3. **主场景和大楼外观 (Title/Scene backgrounds - A1/A13)**:
   - 标题画面中由 Cedar 木纹、石头地基、暖光窗户构成的 6 层大楼主体外观，以及办公室电梯门，这些属于大面积、高精度的背景，必须使用整张 PNG 导入。
4. **系统小图标 (Icons - 📋/💰/👥/⚙)**:
   - 悬浮 HUD 上显示的任务小夹板、金币、人数、设置齿轮等像素图标，需要 PNG/SVG 格式以保证精细度。

### 5.2 可完全使用前端技术 (Canvas/CSS/SVG) 动态生成的素材
这些素材非常适合利用前端算法、Canvas 2D API 或 SVG 动态渲染，不仅大幅**减小打包体积**，还能通过代码**动态调整参数**（如天气、光照强度）：

1. **地板与地砖 (Floors & Grass - 🎮)**:
   - *实现*：使用代码动态生成 16×16 的像素地毯或木地板单元（Pattern），然后利用 Canvas 的 `createPattern` 自动平铺整个地表，免去了加载大面积地板图片的开销。
2. **墙体与隔断屏风 (Walls & Windows - 🎮)**:
   - *实现*：办公室木墙、矮玻璃屏风可以直接通过 `ctx.fillRect` 绘制，加上深褐色的像素边框 (`strokeRect`)，用几条细线模拟玻璃反光和木头纹路即可，完全无需外部图片。
3. **软光影与遮罩效果 (Ambient Glows & Lighting - 🎮)**:
   - *实现*：台灯暖金色的散射光、显示器淡绿色的屏幕反光。使用 Canvas 的径向渐变 (`createRadialGradient`)，以 `screen` 混合模式叠加在角色 and 家具上，呈现逼真的像素微光和日夜阴影。
4. **天气与状态粒子特效 (Rain & Particle Systems - 🎮)**:
   - *实现*：标题画面的下落像素雨、小人打字时漂浮上升的绿色像素颗粒、宕机时从头顶冒出的红色粒子烟雾。这些纯用 Canvas 的定时坐标更新、随机颜色梯度绘制，即可实现 60 帧丝滑粒子动画。
5. **羊皮纸 UI 框架与对话面板 (UI Panels & Boards - 🖥️ 或 🎮)**:
   - *实现*：星露谷风格的木纹公告框、羊皮纸弹窗背景。在 React DOM 中直接使用 CSS 的 `border-image`（拉伸木质边框）配以背景色完成，或是在 Canvas 中绘制圆角矩形，在代码层面非常易于定制。

### 5.3 角色 Sprite

为了让 Agent 在现代办公场景中自然行走（横向走廊、纵向走廊）并在不同朝向的工位坐下，L2 Leader 与 L3 Agent 角色使用**多方向动作精灵图（Multi-directional Sprite Sheet）**，包含：正面/朝下（Front/Down）、背面/朝上（Back/Up）、左侧面（Left Profile）、右侧面（Right Profile）等朝向。

| 素材 | 方式 | 帧数 | 尺寸 | 说明 |
|------|------|------|------|------|
| L1 首席秘书（常驻） | 🎨 | 8帧 | 32×32 | 常驻主工位，仅需正面朝向：4帧 idle + 4帧 working |
| L2 Team Leader | 🎨 | 16帧 (4方向) | 24×24 | 4方向：正面(idle×2, walk×2), 左侧面(walk×2, sit×2), 右侧面(walk×2, sit×2), 背面(walk×2, sit×2) |
| L3 Agent 员工 | 🎨 | 16帧 (4方向) | 24×24 | 4方向：正面(idle×2, walk×2), 左侧面(walk×2, sit×2), 右侧面(walk×2, sit×2), 背面(walk×2, sit×2) |
| 女性/男性变体 | 🎨 | 各一套完整 | 同上 | L1/L2/L3 均需男女独立版本，确保生成质量 |
| L1 秘书大工位 | 🎨 | 静态 + 屏幕亮起帧 | 96×64 | 比普通工位更大、更精致的木纹大理石秘书桌 |

**占位方案**：开发阶段先用纯色矩形 + emoji 替代，后续替换 spritesheet。

**动画帧统一要求 (每方向 4 帧，共 16 帧/角色)**：
- **正面层 (Row 1)**: 2帧正面呼吸浮动 (idle) + 2帧正面走路循环 (walk)
- **左侧层 (Row 2)**: 2帧左侧走路循环 (walk) + 2帧左侧坐姿打字 (sit/work)
- **右侧层 (Row 3)**: 2帧右侧走路循环 (walk) + 2帧右侧坐姿打字 (sit/work)
- **背面层 (Row 4)**: 2帧背面走路循环 (walk) + 2帧背面坐姿打字 (sit/work) - *用于背对玩家的操作*

*注：虽然 Midjourney 生成的精灵图中包含左/右两个方向的侧面以方便挑选，但前端代码为节省资源或处理帧对称性，也可以统一截取其中一侧（如右侧），在向另一侧移动时通过 Canvas 镜像（scaleX: -1）实现翻转。*

---

## 五-A、Google Gemini (Nano Banana) 图片生成 Prompt 库

> 所有 prompt 均针对 Google Gemini 3.1 Flash Image (Nano Banana 2) 及 Imagen 3 系列图像生成模型优化。
> 统一风格要求：**8-bit pixel art, Stardew Valley style warm palette, cozy modern office interior, wood-and-carpet aesthetic**。
> 为了方便 Nano Banana 模型正确理解并生成高质量的像素素材，我们遵循以下提词规范：
> 1. **详尽的自然语言描述（Detailed Natural Language）**：与 Midjourney 偏好扁平 Tag 不同，Nano Banana 对长句、段落和详尽的细节描述有极强的语义理解能力。直接使用流畅的英文长句进行场景与角色的刻画能获得最佳效果。
> 2. **明确的构图与网格说明（Grid & Aspect Ratio Instructions）**：在描述中直接写入宽高比要求（如 `16:9 landscape aspect ratio`）以及网格布局结构（如 `arranged in a neat grid layout`），引导模型生成结构工整的 Sprite Sheet 精灵图。
> 3. **全向控制（Four Directions Poses）**：在 Prompt 中明确要求展示 `facing forward (front view)`, `facing away (back view)`, `profile facing left`, 和 `profile facing right` 四个方向，确保模型生成的精灵图中包含向左和向右的双向行走及工作状态。
> 4. **强制白色底色**：统一使用 `solid white background`，使导出的像素图背景极为纯净，极大地方便了后续导入开发工具中进行抠图和切片（Slice）处理。

### A1. 办公楼外观（Title Scene 背景）

* **用途**：标题画面主视觉，朝阳照耀下的温馨现代办公楼外观，带有一些绿植点缀
* **建议保存路径**：`desktop/src/renderer/assets/backgrounds/building_exterior.png`

```
A 16:9 landscape aspect ratio pixel art scene of a modern office building exterior in bright sunny daylight. The style is 8-bit retro game art reminiscent of Stardew Valley, utilizing a warm color palette of oak wood brown, pine green, sky blue, and parchment cream. The building is a cozy 6-story modern office building made of warm oak wood panels with large glass windows, viewed from the front. The windows have a warm glowing yellow light coming from inside. The base of the building is surrounded by manicured office landscaping, including small green shrub beds. The background features a bright blue sky with soft white clouds and sun rays. Clean pixel art, no anti-aliasing.
```

### A2-M. L1 首席秘书角色 Sprite Sheet (男性)

* **用途**：始终在线的主角色，专属大工位，用户的对话入口（选择男性秘书时的动作序列）
* **建议保存路径**：`desktop/src/renderer/assets/sprites/secretary_male.png`

```
A 16:9 landscape aspect ratio pixel art character sprite sheet for a male chief secretary, 8-bit retro game style with cozy Stardew Valley warm wood colors. The character is a professional male secretary in smart office attire: an oak brown blazer, a creamy parchment shirt, a vibrant green tie, and warm beige skin tone. The sprite sheet displays a sequence of poses arranged in a clean row grid on a solid white background: standing idle, typing efficiently at a desk, looking up attentively, and pensive thinking with a hand on his chin. Clean pixel art with no anti-aliasing.
```

### A2-F. L1 首席秘书角色 Sprite Sheet (女性)

* **用途**：始终在线的主角色，专属大工位，用户的对话入口（选择女性秘书时的动作序列）
* **建议保存路径**：`desktop/src/renderer/assets/sprites/secretary_female.png`

```
A 16:9 landscape aspect ratio pixel art character sprite sheet for a female chief secretary, 8-bit retro game style with cozy Stardew Valley warm wood colors. The character is a professional female secretary in smart office attire: an oak brown blazer or cardigan, a creamy parchment blouse, a green hair accessory, and warm beige skin tone. The sprite sheet displays a sequence of poses arranged in a clean row grid on a solid white background: standing idle, typing efficiently at a desk, looking up attentively, and pensive thinking with a hand on her chin. Clean pixel art with no anti-aliasing.
```

### A3-M. L2 Team Leader 角色 Sprite Sheet (男性)

* **用途**：各团队 Leader，从入口走入→坐到工位→工作→离开（支持多方向行走与坐姿）
* **建议保存路径**：`desktop/src/renderer/assets/sprites/leader_male.png`

```
A 16:9 landscape aspect ratio pixel art character sprite sheet for a male team leader, 8-bit retro game style with a cozy Stardew Valley color palette. The male manager wears business casual attire: a warm oak brown shirt, dark walnut pants, glasses with golden rims, and carries a clipboard. The sprite sheet must be a multi-directional sprite grid showing the character from 4 distinct directions to support full movement: facing forward (front view), facing away (back view), profile facing left, and profile facing right. It should display walk cycles and sitting/typing animations for all 4 directions. Arranged neatly in rows and columns on a solid white background, clean pixel art, no anti-aliasing.
```

### A3-F. L2 Team Leader 角色 Sprite Sheet (女性)

* **用途**：各团队 Leader，从入口走入→坐到工位→工作→离开（支持多方向行走与坐姿）
* **建议保存路径**：`desktop/src/renderer/assets/sprites/leader_female.png`

```
A 16:9 landscape aspect ratio pixel art character sprite sheet for a female team leader, 8-bit retro game style with a cozy Stardew Valley color palette. The female manager wears business casual attire: a warm oak brown blouse, dark walnut pants, glasses with golden rims, and carries a clipboard. The sprite sheet must be a multi-directional sprite grid showing the character from 4 distinct directions to support full movement: facing forward (front view), facing away (back view), profile facing left, and profile facing right. It should display walk cycles and sitting/typing animations for all 4 directions. Arranged neatly in rows and columns on a solid white background, clean pixel art, no anti-aliasing.
```

### A4-M. L3 Agent 角色 Sprite Sheet (男性)

* **用途**：普通员工，从入口走入→坐到工位→执行任务→离开（支持多方向行走与坐姿）
* **建议保存路径**：`desktop/src/renderer/assets/sprites/agent_male.png`

```
A 16:9 landscape aspect ratio pixel art character sprite sheet for a male office worker, 8-bit retro game style with a cozy Stardew Valley color palette. The male worker wears casual office clothing: a warm oak brown hoodie or simple shirt, jeans, and dark chocolate shoes, with warm beige skin tone. The sprite sheet must be a multi-directional sprite grid showing the character from 4 distinct directions to support full movement: facing forward (front view), facing away (back view), profile facing left, and profile facing right. It should show brisk walk cycles and sitting/typing animations at a desk with green computer screen glow reflecting on his face for all 4 directions. Arranged in a neat grid on a solid white background, clean pixel art, no anti-aliasing.
```

### A4-F. L3 Agent 角色 Sprite Sheet (女性)

* **用途**：普通员工，从入口走入→坐到工位→执行任务→离开（支持多方向行走与坐姿）
* **建议保存路径**：`desktop/src/renderer/assets/sprites/agent_female.png`

```
A 16:9 landscape aspect ratio pixel art character sprite sheet for a female office worker, 8-bit retro game style with a cozy Stardew Valley color palette. The female worker wears casual office clothing: a warm oak brown hoodie or simple shirt, jeans, and dark chocolate shoes, with warm beige skin tone. The sprite sheet must be a multi-directional sprite grid showing the character from 4 distinct directions to support full movement: facing forward (front view), facing away (back view), profile facing left, and profile facing right. It should show brisk walk cycles and sitting/typing animations at a desk with green computer screen glow reflecting on her face for all 4 directions. Arranged in a neat grid on a solid white background, clean pixel art, no anti-aliasing.
```

### A5. 电脑显示器 + 办公桌（工位 Tileset - 多方向组件）

* **用途**：每个工位的核心家具，程序化搭建工位的基础素材（支持上、下、左、右四种工位朝向的摆放）
* **建议保存路径**：`desktop/src/renderer/assets/furniture/cubicle_tileset.png`

```
A 16:9 landscape aspect ratio pixel art tileset sheet for a modern office cubicle desk setup, 8-bit retro game style with cozy Stardew Valley colors. The perspective is a top-down RPG view looking down at a 45-degree angle. The sheet includes individual oak brown desks, dark chocolate brown CRT monitors, office keyboards, and rolling ergonomic chairs. All furniture must be shown in multiple orientations to compose workstations facing in 4 directions: facing forward (front view), facing away (back view), profile facing left, and profile facing right. Neatly arranged in a tileset grid on a solid white background, clean pixel art, no anti-aliasing.
```

### A5.5. L1 首席秘书专属工位 (迎客视角/前向摆放)

* **用途**：L1 秘书的大型专属工位，比普通工位更精致显眼。设计为**迎客前向视角**：桌子在前方，椅子在后方，L1 坐在椅子上面向玩家（Front-facing），双显示器呈 45 度角斜放在 L 型桌面两侧，避免遮挡 L1 的正面立绘。
* **建议保存路径**：`desktop/src/renderer/assets/furniture/secretary_desk.png`

```
A 4:3 landscape aspect ratio pixel art of a welcoming, front-facing premium executive secretary desk setup, 8-bit retro game style. Top-down RPG perspective looking down at a 45-degree angle. It features a large, premium L-shaped warm oak wood desk designed as a reception desk where the secretary sits behind the desk facing forward (towards the player). The dual dark chocolate CRT monitors are placed on the side wings of the L-shaped desk, angled 45 degrees inward, so the center of the desk is clear and does not block the secretary's face. Includes a large, comfortable executive rolling chair positioned behind the desk facing the player. On the desk are a coffee mug with steam, documents, a small desk lamp casting warm amber light, and a potted plant. Cozy Stardew Valley-like warm color palette, solid white background, clean pixel art, no anti-aliasing.
```

### A13. 办公楼入口 / 电梯 Spawn 点

* **用途**：Agent spawn/despawn 的视觉锚点
* **建议保存路径**：`desktop/src/renderer/assets/backgrounds/office_entrance.png`

```
A 4:3 landscape aspect ratio pixel art of an office building entrance or elevator lobby, 8-bit retro game style. Front-facing view for a 2D side-scrolling office layout. It features a cozy wooden elevator door frame with oak wood panels, an entrance sign reading "ENTRANCE" in pixel font, a floor indicator showing an arrow, and a textured floor mat. The image must show two states side-by-side: closed elevator doors, and open elevator doors with glowing warm gold light from inside. Cozy Stardew Valley-like warm paneling, solid white background, clean pixel art, no anti-aliasing.
```

### A10. 饮水机（可选装饰）

* **用途**：茶水间/走廊装饰物
* **建议保存路径**：`desktop/src/renderer/assets/furniture/water_cooler.png`

```
A 1:1 square aspect ratio pixel art of a standing office water cooler dispenser, 8-bit retro game style, front view. It features a warm cream body, a semi-transparent blue water jug on top, small red and blue spigots for hot/cold water, and a paper cup dispenser mounted on the side. Cozy Stardew Valley colors, solid white background, clean pixel art, no anti-aliasing.
```

### A11. 绿植盆栽（可选装饰）

* **用途**：办公室装饰，增加场景生动感
* **建议保存路径**：`desktop/src/renderer/assets/furniture/potted_plant.png`

```
A 1:1 square aspect ratio pixel art of a potted office indoor plant, 8-bit retro game style, side view. A medium-sized indoor plant with broad spring green leaves inside a warm oak brown clay terra-cotta pot. Cozy Stardew Valley colors, solid white background, clean pixel art, no anti-aliasing.
```

### A14-M. 首席秘书预览立绘（男性）

* **用途**：首次入职注册时，预览男性 L1 秘书角色的半身像/立绘
* **建议保存路径**：`desktop/src/renderer/assets/portraits/portrait_secretary_male.png`

```
A 1:1 square aspect ratio pixel art character portrait of a male chief secretary, standing in a friendly full-body pose, looking forward. The style is reminiscent of Stardew Valley NPC dialogue portraits and RPG status screen art. The male character wears smart business casual attire: a warm oak brown blazer, a creamy parchment shirt, a green tie, and neat dark hair, with a warm beige skin tone. Solid white background, clean pixel art, no anti-aliasing.
```

### A14-F. 首席秘书预览立绘（女性）

* **用途**：首次入职注册时，预览女性 L1 秘书角色的半身像/立绘
* **建议保存路径**：`desktop/src/renderer/assets/portraits/portrait_secretary_female.png`

```
A 1:1 square aspect ratio pixel art character portrait of a female chief secretary, standing in a friendly full-body pose, looking forward. The style is reminiscent of Stardew Valley NPC dialogue portraits and RPG status screen art. The female character wears smart business casual attire: a warm oak brown blazer or cardigan, a creamy parchment blouse, a green hair clip, and neat dark hair, with a warm beige skin tone. Solid white background, clean pixel art, no anti-aliasing.
```

---

### Prompt 使用说明

1. **生成顺序**：A4-M/F（L3 Agent）→ A3-M/F（L2 Leader）→ A2-M/F（L1 秘书）→ A14-M/F（预览立绘）→ A5（工位）→ A5.5（L1 秘书桌）→ A13（入口）→ A1（办公楼）→ A10-A11
2. **生成模型与适配**：此 Prompt 库专门面向 Google Gemini Image (Nano Banana) 模型优化。请在 Google AI Studio 或相关 API 通道中使用 Gemini 3.1 Flash / Imagen 3 等模型执行图像生成，无需添加任何特定于 Midjourney 的命令行参数（如 `--niji` 或 `--style`）。
3. **性别与方向变体**：角色资产已完全分立男性（-M）与女性（-F）的独立提词。同时，精灵图 Prompt 均以纯句描述方式显式包含了 Front/Down (正面/朝下), Back/Up (背面/朝上), Left Profile (向左侧面), Right Profile (向右侧面) 四个方位的绘制命令，以确保生成的精灵图网格中包含完整的双向对称行走和坐姿形态。
4. **尺寸验证与后期处理**：由于 AI 像素图生成可能存在微小形变，获取图片后建议在 Aseprite 或 Photoshop 中裁剪，并统一项目专属 HSL 调色板（详见 3.2 节）。因为使用了强制白色背景，抠图边缘处理会非常高效。
5. **多版本尝试**：建议单次生成设置 3-4 个变体，挑出切片排列最规整、造型最生动的版本进行精细化修像素。

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
| 部门 | Team（含 L2 Leader） | 隔间办公区 + 木雕招牌 |
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
| working | 正在执行 LLM 调用 | 快速敲键盘，屏幕绿光闪烁，粒子飞出 | 坐在工位 |
| tool_calling | 执行工具 | 手上工具图标切换，屏幕快速闪 | 坐在工位 |
| waiting | 等待用户输入/确认 | 转头看门口，手指轻敲桌面 | 坐在工位 |
| error | LLM 返回错误 | 冒烟/着火 | 坐在工位 |
| walking_out | 任务完成/超时 | walking 动画，向入口移动 | 工位 → 入口路径 |
| despawned | 到达入口消失 | 消失粒子效果 | — |

**状态颜色对应**：
- working：作物绿呼吸光（#4eb036）
- waiting：南瓜黄闪烁（#e28a2b）
- error：浆果红脉冲（#d83838）
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
- [ ] 仿星露谷原木暖色主题 Tailwind 配置
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
┌──────────────────────────────────────────────────────────────────┐
│                     requestAnimationFrame Loop                   │
│                                                                  │
│  ┌──────────┐  ┌────────────┐  ┌────────────┐  ┌────────┐  ┌───┐ │
│  │ WS Cache │  │ Pathfinder │  │   Sprite   │  │ Depth  │  │Rnd│ │
│  │  Message │  │  A* Tick   │  │ Frame Tick │  │Sorting │  │er │ │
│  └──────────┘  └────────────┘  └────────────┘  └────────┘  └───┘ │
│                                                                  │
│  每帧(60 FPS)执行：                                               │
│  1. WS 消息解包更新：读取后台推送，变更 Agent 状态及目标网格    │
│  2. 寻路步进：更新 walking 状态 Agent 的网格坐标与平滑插值      │
│  3. 精灵帧轮转：检查计时器，累加工作/行走/错误中的 Sprite 帧索引 │
│  4. 深度重排：计算深度排序层 (Y-Sorting) 的子节点渲染 z-index     │
│  5. 批量绘制：Konva layer.batchDraw() 更新 Canvas 画面           │
└──────────────────────────────────────────────────────────────────┘
```

**Game Loop 核心细节实现**：

#### 1. 寻路步进与平滑移动 (Pathfinding & Movement)
- **网格系统**：场景划分为 16×16 逻辑网格。
- **A\* 调度**：Agent 的行走路径是用 A\* 算法计算的一组网格点序列 `[{gx1, gy1}, {gx2, gy2}, ...]`。
- **平滑插值**：为了避免小人一格一格跳动，在 Game Loop 中不直接突变坐标，而是设定行走速度，在两点之间进行平滑过渡：
  ```javascript
  // 每一帧根据 delta time 更新 Agent 坐标朝下一个网格点靠近
  const dx = targetX - agent.x;
  const dy = targetY - agent.y;
  const distance = Math.sqrt(dx * dx + dy * dy);
  if (distance > 1) {
    agent.x += (dx / distance) * speed * dt;
    agent.y += (dy / distance) * speed * dt;
    agent.playAnimation('walk');
  } else {
    agent.x = targetX;
    agent.y = targetY;
    agent.nextPathNode(); // 到达网格点，指向序列中的下一个点
  }
  ```

#### 2. 动态精灵帧更新 (Sprite Animation Frame Indexing)
- **非每帧递增**：因为游戏以 60 FPS 渲染，如果每帧更换一张 Sprite 贴图，动作会快到肉眼无法看清。
- **帧计时器**：每个 Sprite 有一个独立的帧时长计时器（如每帧展示 `120ms`）：
  ```javascript
  agent.frameTimer += dt;
  if (agent.frameTimer >= 0.12) { // 超过 120ms，轮转至下一帧
    agent.frameIndex = (agent.frameIndex + 1) % animationFrames[agent.state].length;
    agent.frameTimer = 0;
  }
  ```

#### 3. 深度重排更新 (Depth Sort Throttling)
- 虽然 Y-Sorting 很重要，但在 Canvas 包含数百个元素时，每帧执行快速排序（O(N log N)）会带来不必要的 CPU 消耗。
- **限频运行**：深度重排在小人实际发生纵向移动时触发，或者在 Game Loop 中每隔 `3帧` (50ms) 执行一次，从而极大优化渲染管线性能。

#### 4. 离屏渲染与脏矩形技术 (Offscreen & Batch Drawing)
- 地面层（Layer 0）为纯静态拼贴，只需在初始化时绘制一次并生成缓存 (`layer0.cache()`)。
- 只有动态深度层（Layer 2）和粒子灯光层（Layer 3）需要在 Game Loop 中高频清除并重绘，这通过 react-konva 的 `batchDraw()` 批量收集脏区域并一次性提交给显卡。

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
