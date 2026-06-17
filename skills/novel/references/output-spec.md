# 输出规范（通用）

> 本文件定义技能产出的**所有文件**的路径、格式、前置检查。是章节创作分支 `story-engine` 与世界规划分支 `world-builder` 的产物落地规则。
> 项目红线外挂文件可以**覆盖**本文件中的"项目根 / 体系文档命名"，但不得覆盖"通用路径模板"。

---

## 一、项目目录结构（通用模板）

> 默认以"当前项目目录"为根。如果项目是子目录形式（如 `<project>/tianyuanjian/`），则"项目根"指子目录。项目红线文件 §根目录定位 可以覆盖。

```
<project-root>/
├── novel-project.md          # 项目红线外挂文件（推荐）
├── 核心世界观总纲.md         # 世界观总览（名称可由项目覆盖）
├── 体系文档/*.md             # 各类体系文档（修炼/法宝/人物/地理 等）
├── outlines/
│   ├── 总大纲.md              # 全书总纲
│   └── volume-{N}-outline.md  # 每卷大纲
├── blueprints/
│   └── volume-{N}/
│       └── chapter-{X}-blueprint.md
├── contents/
│   └── volume-{N}/
│       └── chapter-{X}.txt    # 正文，.txt 格式
├── plots/
│   ├── active-hooks/
│   │   └── hook-{名称}.md     # 长线钩子追踪
│   └── milestones/
│       └── vol-{N}-ch-{Y}-summary.md
└── quality-control/
    └── correction-suggestions/
        └── vol-{N}-ch-{X}-quality-report.md
```

---

## 二、路径规范（强制）

| 产物             | 路径模板                                                                      |
| ---------------- | ----------------------------------------------------------------------------- |
| 项目红线         | `<project>/novel-project.md`                                                  |
| 世界观总纲       | `<project>/核心世界观总纲.md`（项目可覆盖文件名）                             |
| 体系文档         | `<project>/{体系名}.md`（直接放项目根，命名由项目定）                         |
| 总大纲           | `outlines/总大纲.md`（名称可由项目覆盖）                                      |
| 卷大纲           | `outlines/volume-{N}-outline.md`                                              |
| 章节蓝图         | `blueprints/volume-{N}/chapter-{X}-blueprint.md`                              |
| 章节正文         | `contents/volume-{N}/chapter-{X}.txt`                                         |
| 活跃钩子         | `plots/active-hooks/hook-{名称}.md`                                           |
| 里程碑           | `plots/milestones/vol-{N}-ch-{Y}-summary.md`                                  |
| 质检报告（单章） | `quality-control/correction-suggestions/vol-{N}-ch-{X}-quality-report.md`     |
| 质检报告（批量） | `quality-control/correction-suggestions/vol-{N}-ch-{A}-{B}-quality-report.md` |

**命名规则**：

- 卷号 `{N}` 用阿拉伯数字，从 1 开始
- 章号 `{X}` / `{Y}` 用两位数字，如 `chapter-01`、`chapter-13`；若超过 99 章用 3 位
- 钩子名 `{名称}` 用简短中文关键词，如 `hook-母亲遗物`、`hook-宗门旧事`

---

## 三、文件格式

| 产物         | 格式              | 语言 |
| ------------ | ----------------- | ---- |
| 章节正文     | `.txt`            | 中文 |
| 其他所有文档 | Markdown（`.md`） | 中文 |

**强制要求**：

- 所有文件必须使用**中文字符**作为主内容（专有名词英文除外）
- 章节正文**不要**使用 Markdown 语法，纯文本即可
- 策划文档（蓝图、里程碑、钩子、大纲）使用规范的 Markdown 结构

---

## 四、前置检查（工作流级别强制）

### 4.1 章节创作工作流前置

进入"章节创作"前**必须**按序完成：

1. 载入项目红线外挂文件（见 [project-override-spec.md](project-override-spec.md)）
2. 读取**最新里程碑**：`plots/milestones/` 下时间戳最新的一份
3. 扫描**所有活跃钩子**：`plots/active-hooks/*.md`，识别本章需要推进 / 回收的
4. 读取**本卷大纲**：`outlines/volume-{N}-outline.md`，确认本章所属 Arc
5. 读取**本章蓝图**：`blueprints/volume-{N}/chapter-{X}-blueprint.md`；若不存在 → 先生成蓝图
6. 读取**相关体系文档**：根据蓝图中"体系文档引用清单"按需读取

**任一失败**：立即停止，告知用户缺失项，提供补齐建议。不得假设并继续。

### 4.2 卷规划工作流前置

1. 载入项目红线
2. 读取总大纲
3. 读取上一卷末尾里程碑（若 N > 1）
4. 读取所有活跃钩子（含未回收的跨卷钩子）

### 4.3 项目初始化工作流前置

1. 检查是否已存在世界观总纲 / 体系文档
2. 如已存在 → 进入"增量确认"模式，**不得覆盖既有设定**
3. 如不存在 → 按世界规划分支 `world-builder` 的构建流程依次产出

---

## 五、产物落地顺序（章节创作工作流）

严格按以下顺序输出，后一步依赖前一步：

```
1. 前置检查（不产出文件，但必须全部 PASS）
   ↓
2. 蓝图（若不存在）：blueprints/volume-{N}/chapter-{X}-blueprint.md
   ↓
3. 蓝图完备性校验（PASS 才能继续）
   ↓
4. 正文：contents/volume-{N}/chapter-{X}.txt
   ↓
5. 质检：quality-control/correction-suggestions/vol-{N}-ch-{X}-quality-report.md
   ↓
6. 红线 PASS（如 FAIL → 回到第 4 步修订，最多 2 轮）
   ↓
7. 钩子更新：plots/active-hooks/hook-{名称}.md（推进 / 回收 / 新建）
   ↓
8. 里程碑（若为 5 章节点）：plots/milestones/vol-{N}-ch-{Y}-summary.md
```

**任一步失败都必须回到上一步修复，不得跳步。**

---

## 六、跨项目可移植性

本技能**自成体系**，不绑定任何具体项目：

- 技能本体只依赖 `.codebuddy/skills/novel/` 下的文件
- 项目特化配置（体系文档名、根目录位置、特殊红线）全部通过**项目红线外挂文件**注入
- 技能不硬编码任何项目名、人物名、地名、体系名

因此，`.codebuddy/skills/novel/` 整个目录可以**直接复制**到任何新的网络小说项目使用。
