# Skill Store 与管理

[English: Skill store and management](../skill_store.md)

我把当前 Skill Registry 放在 internal/agenttools/skill，并从嵌入式 Catalog
和用户的 ~/.soloqueue/skills/ 加载 Skill。

我在 Skills 设置页面查看 Catalog、导入本地 Skill、在支持时从 Git 安装、编辑、
启用或关闭，并查看调用统计。内置 Skill 是只读的；我的本地覆盖放在用户目录。

我要求每个 Skill 包含有效的 SKILL.md。我会使用具体、可执行的描述，避免为每个
请求增加不必要的 Prompt 开销。我使用 soloqueue skills report 检查调用和描述质量。
