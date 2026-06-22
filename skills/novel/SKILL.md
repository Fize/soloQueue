---
name: novel
description: "通用网络小说创作技能 - 支持对小说项目进行初始化、做大纲/蓝图编排、生成章节正文并自动执行风格与词汇红绿线闭环质检。"
when_to_use: "Trigger when the user mentions: 写下一章, 继续写, 写第, 章节创作, 正文创作, 补充钩子, 新增钩子, 更新钩子, 规划卷, 做卷大纲, 安排下一卷"
  - "补充里程碑"
  - "写里程碑"
  - "五章总结"
  - "检查"
  - "校验"
  - "质检"
  - "回归"
  - "初始化"
  - "新建小说"
  - "构建世界观"
  - "剧情"
  - "内容"
  - "人物"
---

# Novel Skill · 网络小说创作

本技能**自成体系**：所有创作方法论沉淀在 `references/` 下，不依赖任何项目自有的 `CODEBUDDY.md` / `CLAUDE.md`；任何项目特化规则通过**项目红线外挂文件**注入，见 `references/project-override-spec.md`。

## 红线一句话声明

**红线不过，章节不交付。** 每次章节创作完成后强制调度内建的**红线质检分支 (quality-editor)** 跑红线清单（通用红线 + 项目红线叠加），任一失败自动回修，最多 2 轮仍失败则人工介入。红黄线清单见 `references/quality-checklist.md`。

## 文件索引

| 文件                                                                       | 用途                                                                            |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| [novel.md](novel.md)                                                       | 主流程：各工作流的「前置检查 → 执行 → 后置产物」三段强制闸门 + 内建分支步骤调度 |
| [references/branch-spec.md](references/branch-spec.md)                     | 技能内建分支步骤（世界规划、章节创作、质量质检）执行规范与硬约束                |
| [references/command-routing.md](references/command-routing.md)             | 自然语言 → 工作流自动路由表                                                     |
| [references/project-override-spec.md](references/project-override-spec.md) | 项目红线外挂文件的发现优先级、加载协议、降级行为                                |
| [references/blueprint-template.md](references/blueprint-template.md)       | 强制蓝图模板（8 模块） + 示例 + 完备性校验规则                                  |
| [references/writing-style.md](references/writing-style.md)                 | 文风法则（猫腻风格配比、智谋主角、情感含蓄、对话机锋）                          |
| [references/anti-ai-patterns.md](references/anti-ai-patterns.md)           | 去 AI 痕迹（禁用词、Show-not-Tell、五感、设定解说 ≤10%、修玄身份言行）          |
| [references/chapter-structure.md](references/chapter-structure.md)         | 凤头/猪肚/豹尾 + 钩子体系（含五章节点强制挂新钩子）                             |
| [references/character-design.md](references/character-design.md)           | 主角三要素 + 配角四问 + 反派四问 + 性别比例                                     |
| [references/conflict-design.md](references/conflict-design.md)             | 五种冲突类型与升级曲线                                                          |
| [references/combat-scene.md](references/combat-scene.md)                   | 打斗场面规范（境界差异、节奏、五感、智慧博弈、自查清单）                        |
| [references/milestone-rules.md](references/milestone-rules.md)             | 里程碑机制（5 章节点、阶段成果 + 下一段钩子双要求）                             |
| [references/output-spec.md](references/output-spec.md)                     | 路径规范、文件格式、前置检查                                                    |
| [references/writing-conventions.md](references/writing-conventions.md)     | 创作节奏、语言规范、度量/时间单位、修玄身份言行                                 |
| [references/quality-checklist.md](references/quality-checklist.md)         | 红线 9 项 + 黄线若干，给**质量质检分支 (quality-editor)** 使用                  |

## 内建分支步骤

本技能支持通过两个分工明确的 Agent 进行正文创作与质量检测，而故事的大纲蓝图编排由我们（创作者/编排者）直接主导：

| 分支步骤                          | 对应 Agent 模板   | 主导工作流                                | 交接产物                                                                                |
| --------------------------------- | ---------------- | ----------------------------------------- | --------------------------------------------------------------------------------------- |
| **故事写手 (story-writer)**       | `story-writer`   | 章节正文创作、精确差值回修自愈              | 章节正文 `contents/volume-{N}/chapter-{X}.txt`                                          |
| **质量质检 (quality-checker)**    | `quality-checker`| 章节红黄线质检、质量回归扫描、回归报告生成  | 质检报告 `quality-control/correction-suggestions/vol-{N}-ch-{X}-quality-report.md`      |

## 核心原则（5 条）

1. **自成体系**：技能不依赖任何项目自有的最高指令集，规则以 `references/` 为单一真相源。
2. **强制闭环**：前置检查不过即停、红线不过即回修、产物缺失即补，全流程不可绕行。
3. **项目红线外挂**：项目特化约束（人物设定红线、身份言行特化、体系文档清单、路径覆盖）通过项目根目录的 `novel-project.md` 或就近红线文件注入，技能自动发现并强制执行。
4. **人物一致性强制**：做卷大纲、写钩子、写正文前，**必须通读**项目 `人物体系/` 下所有相关角色档案。确保剧情发展、人物行为、对话台词完全符合该角色的背景、性格、经历——**这件事、这句话必须是他/她会做、会说的**。人物 OOC（Out of Character）视同红线级别错误。
5. **金庸写作视角强制加载**：进入任意创作工作流（init/plan/write/milestone/hook）时，**必须同时加载 `jin-yong-writing-perspective` 技能**，以金庸"人物驱动情节""情感具象化""道德灰度""间接呈现""求变"等核心心智模型作为创作方法论底层。所有人物刻画、大纲规划、剧情推演、蓝图设计和正文创作，均须调用金庸的原则体系进行推演和决策。

---

> 详细工作流与闸门见 [novel.md](novel.md)。
