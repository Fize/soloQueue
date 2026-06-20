#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys
import os
import re
import json
import argparse

# 禁用词列表 (R1)
BANNED_WORDS = [
    # 基础禁用词
    "需要强调的是", "总的来说", "值得注意的是", "值得一提的是", "从某种意义上说", "从某种意义上来说",
    "不可否认", "不可否认的是", "可以这样理解", "某种程度上", "不难看出", "综上所述",
    "一言以蔽之", "换言之", "换句话说", "毫无疑问", "毫无疑问的是",
    # 1. 转折说教类变体
    "不得不说", "不得不承认", "不得不置信", "说实话",
    "简而言之", "简单来说", "总而言之", "由此可见", "显而易见",
    "与此同时的是", "也就是说",
    # 2. 过渡时间流逝变体
    "随着时间的推移", "时光荏苒", "光阴似箭",
    "在接下来的时间里", "在接下来的日子里", "接下来的剧情中",
    "随之而来的是", "随之而来的",
    # 3. 人物动作与情绪套路
    "微微一笑", "嘴角微微上扬", "露出一抹笑意", "淡然一笑", "会心一笑", "嘴角微扬",
    "深吸一口气", "深吸气", "吸了一口气",
    "闪过一丝", "掠过一丝", "浮现出一丝",
    "不仅如此", "不仅仅是",
    "不禁", "忍不住"
]

# 现代词正则匹配 (R9)
MODERN_PATTERNS = [
    (re.compile(r"秒(?!针|退|杀|懂)"), "秒（现代时间单位，建议改用‘瞬息’、‘刹那’、‘弹指’）"),
    (re.compile(r"分钟"), "分钟（现代时间单位，建议改用‘一炷香’、‘半个时辰’）"),
    (re.compile(r"小时"), "小时（现代时间单位，建议改用‘时辰’）"),
    (re.compile(r"分米|厘米|毫米|千米|公里"), "现代长度单位（建议改用‘寸’、‘尺’、‘丈’、‘里’等）"),
    (re.compile(r"(?:\d+|[一二三四五六七八九十百千])米(?![饭粒粮仓面醋贴大稻小柴送粳糯粟薏玉花生])"), "米（现代长度单位，建议改用‘尺’、‘丈’）"),
    (re.compile(r"克|千克|公斤|吨"), "现代重量单位（建议改用‘斤’、‘两’、‘石’）"),
    (re.compile(r"计算机|数据库|网络|信号|系统"), "科技/现代词汇（修玄题材禁止出现）"),
    (re.compile(r"订单|客户|业绩|KPI|营业执照|税务|流程|效率|团队|数据|概率"), "商业/现代词汇（修玄题材禁止出现）"),
]

# 内心独白动词 (R10a)
MENTAL_PATTERNS = [
    (re.compile(r"[他她它我你](?:们)?(?:想|感到|意识到|回忆起|觉得|认为|明白|想道|思索|琢磨)"), "内心独白词（应转化为外部物理反应或环境描写）"),
    (re.compile(r"心中(?:想|感到|意识到|回忆起|觉得|认为|明白|思索|琢磨)"), "内心独白词（应转化为外部物理反应或环境描写）"),
    (re.compile(r"脑海中(?:想|感到|意识到|回忆起|觉得|认为|明白)"), "内心独白词（应转化为外部物理反应或环境描写）"),
]

# 解释性句式 (R10b)
INTERPRET_PATTERNS = [
    (re.compile(r"不是.*?而是"), "“不是...而是...”解释性句式（叙述者替读者做阅读理解，应直接展示画面）"),
    (re.compile(r"并非.*?而是"), "“并非...而是...”解释性句式（叙述者替读者做阅读理解，应直接展示画面）"),
    (re.compile(r"并不是.*?而是"), "“并不是...而是...”解释性句式（叙述者替读者做阅读理解，应直接展示画面）"),
    (re.compile(r"与其说是.*?不如说是"), "“与其说是...不如说是...”解释性句式"),
    (re.compile(r"不在于.*?而在于"), "“不在于...而在于...”解释性句式"),
    (re.compile(r"不在.*?而在"), "“不在...而在...”解释性句式"),
    (re.compile(r"没看.*?看的是"), "“没看...看的是...”解释性句式"),
    (re.compile(r"没有看.*?看的是"), "“没有看...看的是...”解释性句式"),
    (re.compile(r"不是为了.*?而是为了"), "“不是为了...而是为了...”解释性句式"),
    (re.compile(r"非但没有.*?反倒"), "“非但没有...反倒...”解释性句式"),
    (re.compile(r"非但不是.*?反而是"), "“非但不是...反而是...”解释性句式"),
    (re.compile(r"不单单是.*?更是"), "“不单单是...更是...”解释性句式"),
    (re.compile(r"不单是.*?更是"), "“不单是...更是...”解释性句式"),
]

# 豹尾 AI 常用套话 (R4)
AI_ENDING_PATTERNS = [
    (re.compile(r"一切才(?:刚刚?|刚)?开始"), "“一切才刚刚/开始”AI套话（收尾公式化，禁止出现）"),
    (re.compile(r"注定不平静"), "“注定不平静”AI套话（收尾公式化，禁止出现）"),
    (re.compile(r"这只是开始"), "“这只是开始”AI套话（收尾公式化，禁止出现）"),
    (re.compile(r"拉开了序幕"), "“拉开了序幕”AI套话（收尾公式化，禁止出现）"),
    (re.compile(r"翻开了新的一页"), "“翻开了新的一页”AI套话（收尾公式化，禁止出现）"),
    (re.compile(r"注定是一个"), "“注定是一个”AI套话（收尾公式化，禁止出现）"),
    (re.compile(r"注定不平凡"), "“注定不平凡”AI套话（收尾公式化，禁止出现）"),
]

# 叙事距离一致跨时间总结词 (R10c)
SPATIAL_DISTANCE_PATTERNS = [
    (re.compile(r"永远"), "“永远”总结词（全知视角跨时间概括，非对话行禁止使用）"),
    (re.compile(r"总是"), "“总是”总结词（全知视角跨时间概括，非对话行禁止使用）"),
    (re.compile(r"从不"), "“从不”总结词（全知视角跨时间概括，非对话行禁止使用）"),
    (re.compile(r"从.*?起就"), "“从...起就”总结词（全知视角跨时间概括，非对话行禁止使用）")
]

# 叙述者越界弱推理词 (R10b 追加)
WEAK_INFERENCE_PATTERNS = [
    (re.compile(r"大概"), "“大概”推理词（叙述者对事实做推理，非对话行禁止使用）"),
    (re.compile(r"应该"), "“应该”推理词（叙述者对事实做推理，非对话行禁止使用）"),
    (re.compile(r"或许"), "“或许”推理词（叙述者对事实做推理，非对话行禁止使用）"),
    (re.compile(r"恐怕"), "“恐怕”推理词（叙述者对事实做推理，非对话行禁止使用）"),
    (re.compile(r"想必"), "“想必”推理词（叙述者对事实做推理，非对话行禁止使用）")
]


# ==============================================================================
# 笔力特征静态量化规则与词库
# ==============================================================================

# 经典优雅四字成语（常用于武侠/玄幻/古典文学）
COMMON_CLASSICAL_IDIOMS = {
    "气吞山河", "风云变色", "惊天动地", "翻江倒海", "移山填海", "烟消云散", "瞬息万变", "沧海桑田", "势如破竹",
    "摧枯拉朽", "游刃有余", "脱胎换骨", "洗髓伐毛", "返璞归真", "登峰造极", "出神入化", "炉火纯青", "大智若愚",
    "玄之又玄", "众妙之门", "顺天应人", "物极必反", "太上忘情", "斩草除根", "杀伐果断", "狂风暴雨", "雷霆万钧",
    "遮天蔽日", "天崩地裂", "虚无缥缈", "浩瀚无垠", "气吞万里", "气势磅礴", "顶天立地", "斗转星移", "沧海一粟",
    "白驹过隙", "弹指之间", "瞬息之间", "神采奕奕", "威风凛凛", "杀气腾腾", "气宇轩昂", "气度非凡", "仙风道骨",
    "鹤发童颜", "超凡脱俗", "飘然若仙", "凡胎肉身", "凡夫俗子", "芸芸众生", "泥牛入海", "石沉大海", "灰飞烟灭",
    "魂飞魄散", "生死存亡", "命悬一线", "危在旦夕", "迫在眉睫", "千钧一发", "一触即发", "动人心弦", "惊心动魄",
    "扣人心弦", "荡气回肠", "豪气干云", "铁骨铮铮", "侠骨柔情", "血雨腥风", "刀光剑影", "飞沙走石", "地动山摇",
    "惊涛骇浪", "波澜壮阔", "波涛汹涌", "浩浩荡荡", "势不可挡", "势单力薄", "孤掌难鸣", "众志成城", "同心协力",
    "万众一心", "万无一失", "决胜千里", "神机妙算", "料事如神", "未雨绸缪", "防患未然", "亡羊补牢", "塞翁失马",
    "祸福相依", "否极泰来", "乐极生悲", "悲喜交加", "喜出望外", "大喜过望", "欣喜若狂"
}

# 易经/道德经经典道家词汇与修真专用词
CLASSICAL_DAOIST_WORDS = {
    # 易经卦象与哲理
    "太极", "阴阳", "乾坤", "无为", "道法", "自然", "虚无", "混沌", "逍遥", "刚柔", "动静", "天道", "地道",
    "气机", "虚极", "静笃", "损益", "否泰", "既济", "未济", "两仪", "四象", "八卦", "九宫", "变化", "常理",
    "玄之又玄", "众妙之门", "上善若水", "物极必反", "顺天应人", "虚实", "阖辟", "生克", "常静", "常清",
    # 导引与修玄词汇
    "洗髓", "炼气", "筑基", "结丹", "元婴", "化神", "出窍", "合体", "渡劫", "大乘", "羽化", "飞升", "坐忘",
    "心斋", "守一", "服气", "吐纳", "导引", "辟谷", "内丹", "外丹", "金丹", "真元", "法力", "神识", "识海",
    "气海", "丹田", "泥丸", "天门", "玄关", "脉络", "经脉", "罡风", "真火", "雷劫", "心魔", "天劫", "因果",
    "轮回", "宿命", "劫数", "劫难", "机缘", "福地", "洞天", "灵脉", "宗门", "洞府", "散修", "仙人", "凡人",
    "造化", "造物", "苍生", "造化弄人", "返璞归真", "太上忘情", "大智若愚", "无心", "有为", "至人", "神人",
    "圣人", "真人", "飘逸", "洒脱", "超然", "清静", "淡泊", "希夷", "微妙", "玄理"
}

# 旁观者与反应词（用于侧面烘托比率）
BYSTANDER_WORDS = {
    "众人", "围观", "旁人", "看客", "围观者", "窃窃私语", "议论", "面色", "震惊", "骇然",
    "动容", "倒吸", "变色", "惊叹", "瞩目", "旁观", "惊呼", "哗然", "喧哗", "倒吸一口凉气"
}

def detect_idioms(text):
    found = []
    blocks = re.findall(r"[\u4e00-\u9fa5]{4,}", text)
    for block in blocks:
        for i in range(len(block) - 3):
            word = block[i:i+4]
            # 1. 结构匹配 AABB
            if word[0] == word[1] and word[2] == word[3] and word[0] != word[2]:
                found.append((word, "AABB结构"))
            # 2. 结构匹配 ABAC
            elif word[0] == word[2] and word[1] != word[3] and word[0] != word[1]:
                found.append((word, "ABAC结构"))
            # 3. 结构匹配 ABCA
            elif word[0] == word[3] and word[0] != word[1] and word[1] != word[2]:
                found.append((word, "ABCA结构"))
            # 4. 经典成语表匹配
            elif word in COMMON_CLASSICAL_IDIOMS:
                found.append((word, "经典成语"))
    return found

def detect_daoist_words(text):
    found = []
    for word in CLASSICAL_DAOIST_WORDS:
        matches = list(re.finditer(re.escape(word), text))
        for m in matches:
            found.append((word, m.start()))
    
    found.sort(key=lambda x: len(x[0]), reverse=True)
    selected = []
    used_indices = set()
    for word, start in found:
        end = start + len(word)
        if any(idx in used_indices for idx in range(start, end)):
            continue
        selected.append((word, start))
        for idx in range(start, end):
            used_indices.add(idx)
    return selected

def calculate_bystander_ratio(paragraphs, character_names):
    action_count = 0
    reaction_count = 0
    
    action_keywords = {"掌", "剑", "劲", "招式", "出手", "轰鸣", "杀意", "法宝", "斩", "裂", "碎", "破", "迎击", "闪躲", "拳", "指", "诀", "法力", "真元", "灵力", "神通"}
    
    name_counts = {}
    if character_names:
        for p in paragraphs:
            for name in character_names:
                count = len(re.findall(re.escape(name), p))
                if count > 0:
                    name_counts[name] = name_counts.get(name, 0) + count
    sorted_names = sorted(name_counts.items(), key=lambda x: x[1], reverse=True)
    detected_fighters = {n for n, c in sorted_names[:2]}
    bystander_names = character_names - detected_fighters if character_names else set()
    
    for p in paragraphs:
        is_action = any(kw in p for kw in action_keywords)
        has_bystander_word = any(bw in p for bw in BYSTANDER_WORDS)
        has_bystander_name = any(bn in p for bn in bystander_names)
        
        is_reaction = has_bystander_word or has_bystander_name
        
        if is_action:
            action_count += 1
        if is_reaction:
            reaction_count += 1
            
    if action_count == 0:
        return 0.0, 0, 0
    return float(reaction_count) / float(action_count), action_count, reaction_count

def is_dialogue(text):
    text = text.strip()
    return text.startswith(('“', '"', '「', '‘'))

def run_lint(file_path, project_root=None, custom_banned_words=None, trigger_flags=None, config=None):
    if not os.path.exists(file_path):
        return {
            "status": "ERROR",
            "message": f"文件不存在: {file_path}",
            "errors": [],
            "warnings": []
        }

    with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
        lines = f.readlines()

    errors = []
    warnings = []

    # 合并通用禁用词与外挂的自定义禁用词
    active_banned_words = list(BANNED_WORDS)
    if custom_banned_words:
        # 去重合并
        for w in custom_banned_words:
            if w not in active_banned_words:
                active_banned_words.append(w)

    # 识别最后五个非空段落，用以检测豹尾套话 R4
    non_empty_indices = [i for i, l in enumerate(lines) if l.strip()]
    last_5_indices = set(non_empty_indices[-5:]) if len(non_empty_indices) >= 5 else set(non_empty_indices)

    for idx, raw_line in enumerate(lines):
        line_num = idx + 1
        line = raw_line.strip()
        if not line:
            continue

        # 1. 检查段落行数 (R2 / Y6)
        # 假设 30 个字为一行（移动端适配宽度）
        char_count = len(line)
        is_dial = is_dialogue(line)
        if not is_dial:
            if char_count > 240: # 8行以上
                errors.append({
                    "rule": "R2",
                    "line": line_num,
                    "content": line[:30] + "...",
                    "problem": f"段落字数过长 ({char_count} 字，折合超过 8 行)",
                    "fix": "切分成更短的段落，保持在 3-5 行（150字以内）"
                })
            elif char_count > 180: # 6-8行
                warnings.append({
                    "rule": "Y6",
                    "line": line_num,
                    "content": line[:30] + "...",
                    "problem": f"段落字数较长 ({char_count} 字，折合约 6-8 行)",
                    "fix": "建议在适当处切分段落，提升移动端阅读体验"
                })

            # R10c 跨时间总结词检查
            for pattern, desc in SPATIAL_DISTANCE_PATTERNS:
                for m in pattern.finditer(line):
                    errors.append({
                        "rule": "R10c",
                        "line": line_num,
                        "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                        "problem": f"叙事距离不一致 (包含全知总结词): “{m.group(0)}”",
                        "fix": f"修改为描述当下画面。建议: {desc}"
                    })

            # R10b 额外推理词检查
            for pattern, desc in WEAK_INFERENCE_PATTERNS:
                for m in pattern.finditer(line):
                    errors.append({
                        "rule": "R10b",
                        "line": line_num,
                        "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                        "problem": f"发现越界弱推理词 (叙述者做主观猜测): “{m.group(0)}”",
                        "fix": f"改写为客观事实展现。建议: {desc}"
                    })
        else:
            # T4 强情绪对白字数检验
            if trigger_flags and 'T4' in trigger_flags:
                quotes = re.findall(r"[“\"「‘]([^”\"」’]*)[”\"」’]", line)
                for q_text in quotes:
                    if len(q_text) > 50:
                        errors.append({
                            "rule": "T4",
                            "line": line_num,
                            "content": q_text[:30] + "...",
                            "problem": f"强情绪场景下对白字数超限 ({len(q_text)} 字，上限 50 字)",
                            "fix": "缩减对白字数，将其转化为物理动作、神态或视线碰撞泄压"
                        })

        # 2. R1 禁用词检查
        for word in active_banned_words:
            if word in line:
                # 找出所有匹配
                matches = re.finditer(re.escape(word), line)
                for m in matches:
                    errors.append({
                        "rule": "R1",
                        "line": line_num,
                        "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                        "problem": f"包含反 AI 禁用词/套路词: “{word}”",
                        "fix": "删除该词，改用具体的动作、神态或事实描写展示"
                    })

        # 3. R9 现代词污染检查
        for pattern, desc in MODERN_PATTERNS:
            for m in pattern.finditer(line):
                errors.append({
                    "rule": "R9",
                    "line": line_num,
                    "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                    "problem": f"发现现代/科技/商业词汇污染: “{m.group(0)}”",
                    "fix": f"替换为修玄词汇。建议: {desc}"
                })

        # 4. R10a 内心独白检查
        for pattern, desc in MENTAL_PATTERNS:
            for m in pattern.finditer(line):
                errors.append({
                    "rule": "R10a",
                    "line": line_num,
                    "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                    "problem": f"直接使用内心独白动词: “{m.group(0)}”",
                    "fix": "删除心理词，将内心活动转化为外部细节（如眼神、动作、环境变化等）"
                })

        # 5. R10b 解释性句式检查
        for pattern, desc in INTERPRET_PATTERNS:
            for m in pattern.finditer(line):
                errors.append({
                    "rule": "R10b",
                    "line": line_num,
                    "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                    "problem": f"使用了解释性/否定句式: “{m.group(0)}”",
                    "fix": "去掉作者视角的解释，只保留可见的画面，让读者自己理解"
                })

        # R4 豹尾套话检查 (仅在最后 5 个非空段落内生效)
        if idx in last_5_indices:
            for pattern, desc in AI_ENDING_PATTERNS:
                for m in pattern.finditer(line):
                    errors.append({
                        "rule": "R4",
                        "line": line_num,
                        "content": line[max(0, m.start()-10):min(len(line), m.end()+10)],
                        "problem": f"豹尾段落包含 AI 常用套话: “{m.group(0)}”",
                        "fix": f"删除或改写。建议: {desc}"
                    })

    # 6. R8 里程碑强门禁检查（5的倍数章节必须归档且格式正确）
    vol_match = re.search(r"volume[-_](\d+)", file_path, re.IGNORECASE)
    ch_match = re.search(r"chapter[-_](\d+)", file_path, re.IGNORECASE)
    vol = int(vol_match.group(1)) if vol_match else 1
    ch = int(ch_match.group(1)) if ch_match else 1

    if ch % 5 == 0:
        root_dir = project_root if project_root else os.getcwd()
        milestone_rel = os.path.join("plots", "milestones", f"vol-{vol}-ch-{ch}-summary.md")
        milestone_path = os.path.join(root_dir, milestone_rel)
        if not os.path.exists(milestone_path):
            errors.append({
                "rule": "R8",
                "line": 0,
                "content": "",
                "problem": f"缺失里程碑总结文件: {milestone_rel}",
                "fix": f"第 {ch} 章是五章节点，必须在 {milestone_rel} 创建并补齐 A、B 双节。"
            })
        else:
            try:
                with open(milestone_path, "r", encoding="utf-8", errors="ignore") as mf:
                    m_content = mf.read()
                has_a = re.search(r"A\s*节|阶段成果", m_content, re.IGNORECASE) is not None
                has_b = re.search(r"B\s*节|下一阶段入口", m_content, re.IGNORECASE) is not None
                if not (has_a and has_b):
                    errors.append({
                        "rule": "R8",
                        "line": 0,
                        "content": "",
                        "problem": f"里程碑文件 {milestone_rel} 格式不合规，缺少 A 节（阶段成果）或 B 节（下一阶段入口）",
                        "fix": "确保里程碑文件同时包含 A 节（阶段成果）和 B 节（下一阶段入口）标题与详细内容。"
                    })
            except Exception as e:
                errors.append({
                    "rule": "R8",
                    "line": 0,
                    "content": "",
                    "problem": f"无法读取里程碑文件: {milestone_rel} (错误: {str(e)})",
                    "fix": "请检查该文件状态与读取权限。"
                })

    # ==============================================================================
    # 笔力特征静态指标量化与评估
    # ==============================================================================
    full_text = "".join(lines)
    clean_text = re.sub(r"\s+", "", full_text)
    char_count = len(clean_text)
    
    idiom_count = 0
    idiom_density = 0.0
    daoist_count = 0
    daoist_density = 0.0
    bystander_ratio = 0.0
    action_count = 0
    reaction_count = 0
    
    if char_count > 0:
        found_idioms = detect_idioms(clean_text)
        idiom_count = len(found_idioms)
        idiom_density = (idiom_count * 1000.0) / char_count
        
        found_daoist = detect_daoist_words(clean_text)
        daoist_count = len(found_daoist)
        daoist_density = (daoist_count * 1000.0) / char_count
        
        character_names = set()
        char_dir = os.path.join(project_root, "人物体系") if project_root else None
        if char_dir and os.path.exists(char_dir):
            for f_name in os.listdir(char_dir):
                if f_name.endswith(".md"):
                    character_names.add(f_name[:-3])
                    
        paragraphs = [l.strip() for l in lines if l.strip()]
        bystander_ratio, action_count, reaction_count = calculate_bystander_ratio(paragraphs, character_names)

    # 读取阈值指标
    idiom_min = config.get("idiom_density_min", 8.0) if config else 8.0
    idiom_max = config.get("idiom_density_max", 22.0) if config else 22.0
    daoist_min = config.get("daoist_density_min", 15.0) if config else 15.0
    daoist_max = config.get("daoist_density_max", 40.0) if config else 40.0
    bystander_min = config.get("bystander_ratio_min", 0.15) if config else 0.15
    genre = config.get("genre", "修真") if config else "修真"

    if idiom_density < idiom_min:
        warnings.append({
            "rule": "Y9",
            "line": 0,
            "content": "",
            "problem": f"四字成语密度偏低 ({idiom_density:.2f}/千字，低于基线下限 {idiom_min:.1f})",
            "fix": "适当增加成语或精炼四字格句法，提升半文半白文风质感。"
        })
    elif idiom_density > idiom_max:
        warnings.append({
            "rule": "Y9",
            "line": 0,
            "content": "",
            "problem": f"四字成语密度过高 ({idiom_density:.2f}/千字，高于基线上限 {idiom_max:.1f})",
            "fix": "减少成语堆砌，改用自然洗练的日常动作描写。"
        })

    if daoist_density < daoist_min:
        warnings.append({
            "rule": "Y10",
            "line": 0,
            "content": "",
            "problem": f"道家经典/古风词密度偏低 ({daoist_density:.2f}/千字，低于基线下限 {daoist_min:.1f})",
            "fix": f"适当引入来自道家经典（易经、道德经）或修真特化词汇，当前题材为 {genre}。"
        })
    elif daoist_density > daoist_max:
        warnings.append({
            "rule": "Y10",
            "line": 0,
            "content": "",
            "problem": f"道家经典/古风词密度过高 ({daoist_density:.2f}/千字，高于基上限 {daoist_max:.1f})",
            "fix": "减少修辞与玄学概念堆砌，避免词藻过密导致阅读疲劳。"
        })

    if trigger_flags and 'T1' in trigger_flags:
        if bystander_ratio < bystander_min:
            warnings.append({
                "rule": "Y11",
                "line": 0,
                "content": "",
                "problem": f"战斗场景侧面烘托（配角/围观反应）比率偏低 ({bystander_ratio*100:.1f}%，低于要求 {bystander_min*100:.1f}%)",
                "fix": "在精彩出手或强敌压迫时，增加旁观者震惊反应、表情变化或环境细节震荡，折射战斗激烈程度。"
            })

    status = "FAIL" if errors else "PASS"

    return {
        "status": status,
        "errors": errors,
        "warnings": warnings,
        "total_lines": len(lines),
        "idiom_count": idiom_count,
        "idiom_density": idiom_density,
        "daoist_count": daoist_count,
        "daoist_density": daoist_density,
        "bystander_ratio": bystander_ratio,
        "action_count": action_count,
        "reaction_count": reaction_count
    }

def generate_markdown_report(result, vol, ch):
    score = 10 - min(10, len(result["errors"]))
    status_str = "PASS" if result["status"] == "PASS" else "FAIL"

    report = []
    report.append(f"# 质量检查报告 · 卷 {vol} · 第 {ch} 章\n")
    report.append("## 总览\n")
    report.append(f"- **总分**：{score}/10")
    report.append(f"- **红线结果**：{status_str}")
    report.append(f"- **黄线结果**：{len(result['warnings'])} 项需注意")
    report.append(f"- **四字成语密度**：{result.get('idiom_density', 0.0):.2f}/千字 (共 {result.get('idiom_count', 0)} 个)")
    report.append(f"- **道家经典/古风词密度**：{result.get('daoist_density', 0.0):.2f}/千字 (共 {result.get('daoist_count', 0)} 个)")
    if result.get('action_count', 0) > 0:
        report.append(f"- **侧面烘托比率**：{result.get('bystander_ratio', 0.0)*100:.1f}% (动作 {result.get('action_count', 0)} 段，反应 {result.get('reaction_count', 0)} 段)")
    report.append("")

    report.append("## 红线项清单\n")
    report.append("| # | 项目 | 状态 | 位置 | 问题 | 修正建议 |")
    report.append("| --- | --- | --- | --- | --- | --- |")

    # 汇总红线项，包括 R8
    r_rules = {"R1": "零禁用词", "R2": "段落限制", "R8": "里程碑归档", "R9": "无现代词", "R10a": "无内心独白", "R10b": "无解释句式"}
    rule_status = {k: "PASS" for k in r_rules.keys()}
    for err in result["errors"]:
        if err["rule"] in rule_status:
            rule_status[err["rule"]] = "FAIL"

    # 先输出整体状态
    for r_code, r_name in r_rules.items():
        if rule_status[r_code] == "PASS":
            report.append(f"| {r_code} | {r_name} | PASS | - | - | - |")
        else:
            # 找到具体错误输出
            for err in result["errors"]:
                if err["rule"] == r_code:
                    loc = f"第 {err['line']} 行" if err["line"] > 0 else "章节级"
                    context_snippet = f"匹配到: {err['content']} <br> " if err["content"] else ""
                    report.append(f"| {r_code} | {r_name} | FAIL | {loc} | {context_snippet}**问题**: {err['problem']} | {err['fix']} |")

    report.append("\n## 触发标记专项（说明）\n")
    report.append("*(注: 战斗/突破/情感等专项红线需结合蓝图 M7 进行审核)*\n")

    report.append("## 黄线项清单\n")
    report.append("| # | 项目 | 状态 | 位置 | 问题 | 建议 |")
    report.append("| --- | --- | --- | --- | --- | --- |")
    
    y_rules = {
        "Y6": "段落字数预警",
        "Y9": "四字成语密度",
        "Y10": "道家经典/古风词密度",
        "Y11": "侧面烘托比率"
    }
    
    if not result["warnings"]:
        report.append("| Y6 | 段落字数预警 | PASS | - | - | - |")
    else:
        for warn in result["warnings"]:
            rule_name = y_rules.get(warn['rule'], "风格预警")
            loc = f"第 {warn['line']} 行" if warn['line'] > 0 else "章节级"
            report.append(f"| {warn['rule']} | {rule_name} | 需注意 | {loc} | {warn['problem']} | {warn['fix']} |")

    report.append("\n## 结论\n")
    if result["status"] == "PASS":
        report.append("- [x] 红线全 PASS → 章节可交付")
    else:
        report.append("- [ ] 红线有 FAIL → 触发回修")

    return "\n".join(report)

def run_blueprint_lint(file_path, project_root=None):
    if not os.path.exists(file_path):
        return {
            "status": "ERROR",
            "message": f"文件不存在: {file_path}",
            "errors": [],
            "warnings": []
        }

    with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
        content = f.read()

    errors = []

    # 解析 Frontmatter 中的 complexity，默认 medium
    complexity = "medium"
    fm_match = re.match(r"^---(.*?)---", content, re.DOTALL)
    if fm_match:
        fm_text = fm_match.group(1)
        comp_match = re.search(r"complexity\s*:\s*(\w+)", fm_text)
        if comp_match:
            complexity = comp_match.group(1).strip().lower()

    # R0: 校验蓝图是否包含具体对白或正文风格描写句
    blueprint_body = content
    if fm_match:
        blueprint_body = content[fm_match.end():]
    
    blueprint_lines = blueprint_body.split("\n")
    for b_idx, b_line in enumerate(blueprint_lines):
        b_line_stripped = b_line.strip()
        if not b_line_stripped:
            continue
        # 拦截引号对白
        if re.search(r"[“\"「‘][^”\"」’]+[”\"」’]", b_line_stripped):
            errors.append({
                "rule": "R0",
                "problem": f"蓝图中包含具体对白/引号包裹台词: “{b_line_stripped}”",
                "fix": "蓝图仅用于表现方向和结构，禁止预写正文级别对话"
            })
        # 拦截描述句式
        if re.search(r"(?:低声|冷冷|缓缓|沉声)道[：:]", b_line_stripped):
            errors.append({
                "rule": "R0",
                "problem": f"蓝图中出现正文描摹句式: “{b_line_stripped}”",
                "fix": "移除正文细节，替换为宏观方向指示"
            })

    # B1: 校验九大模块存在性 (Low 复杂度下省略 3 和 6)
    required_modules = [
        (re.compile(r"^##\s+1\.\s+基础信息与目标", re.MULTILINE), "模块 1 · 基础信息与目标"),
        (re.compile(r"^##\s+2\.\s+场景设计", re.MULTILINE), "模块 2 · 场景设计"),
        (re.compile(r"^##\s+3\.\s+情绪节奏", re.MULTILINE), "模块 3 · 情绪节奏"),
        (re.compile(r"^##\s+4\.\s+(?:记忆点|叙事重点)", re.MULTILINE), "模块 4 · 叙事重点/记忆点"),
        (re.compile(r"^##\s+5\.\s+钩子进展", re.MULTILINE), "模块 5 · 钩子进展"),
        (re.compile(r"^##\s+6\.\s+章节结构", re.MULTILINE), "模块 6 · 章节结构"),
        (re.compile(r"^##\s+7\.\s+触发标记", re.MULTILINE), "模块 7 · 触发标记与写作约束"),
        (re.compile(r"^##\s+8\.\s+上下文准备", re.MULTILINE), "模块 8 · 上下文准备"),
        (re.compile(r"^##\s+9\.\s+章节结算输出", re.MULTILINE), "模块 9 · 章节结算输出")
    ]

    for pattern, name in required_modules:
        if complexity == "low" and ("情绪节奏" in name or "章节结构" in name):
            continue
        if not pattern.search(content):
            errors.append({
                "rule": "B1",
                "problem": f"缺失必要模块: “{name}” (当前复杂度: {complexity})",
                "fix": f"请在蓝图中以二级标题（##）格式补齐该模块，例如：## {name[3:]}"
            })

    # B2: M1 叙事目标：检查是否非空
    m1_match = re.search(r"##\s+1\.\s+基础信息与目标(.*?)(?=##\s+\d+\.|\Z)", content, re.DOTALL)
    if m1_match:
        m1_content = m1_match.group(1).strip()
        target_match = re.search(r"叙事目标[：:]\s*(.*)", m1_content)
        if not target_match or not target_match.group(1).strip():
            errors.append({
                "rule": "B2",
                "problem": "模块 1 缺失【叙事目标】字段或内容为空",
                "fix": "在‘基础信息与目标’模块下添加：‘- 叙事目标：描述人物动机与物理动作’"
            })

    # B3: M3 情绪节奏：位置点是否 >= 3 (Low 级别免检)
    if complexity != "low":
        m3_match = re.search(r"##\s+3\.\s+情绪节奏(.*?)(?=##\s+\d+\.|\Z)", content, re.DOTALL)
        if m3_match:
            m3_content = m3_match.group(1).strip()
            table_lines = [l for l in m3_content.split("\n") if "|" in l]
            data_rows = len(table_lines) - 2 if len(table_lines) >= 2 else 0
            if data_rows < 3:
                errors.append({
                    "rule": "B3",
                    "problem": f"情绪节奏的位置点过少（当前检测到数据行：{data_rows} 行，要求至少 3 行）",
                    "fix": "在情绪节奏表格中添加更多推进节点，确保情绪走向包含至少 3 个位置点"
                })

    # B4: M2 场景设计
    m2_match = re.search(r"##\s+2\.\s+场景设计(.*?)(?=##\s+\d+\.|\Z)", content, re.DOTALL)
    if m2_match:
        m2_content = m2_match.group(1).strip()
        if "地点" not in m2_content and "场景" not in m2_content:
            errors.append({
                "rule": "B4",
                "problem": "模块 2 场景设计为空或未按格式声明",
                "fix": "在场景设计下填入具体场景信息，包括出场角色与核心事件"
            })

    # B5: M4 叙事重点
    m4_match = re.search(r"##\s+4\.\s+(?:记忆点|叙事重点)(.*?)(?=##\s+\d+\.|\Z)", content, re.DOTALL)
    if m4_match:
        m4_content = m4_match.group(1).strip()
        if len(m4_content) < 10:
            errors.append({
                "rule": "B5",
                "problem": "模块 4 叙事重点为空或内容过短",
                "fix": "补充具体的叙事重点与上帝视角实现手法"
            })

    # B7: M6 章节结构 (Low 级别免检)
    if complexity != "low":
        m6_match = re.search(r"##\s+6\.\s+章节结构(.*?)(?=##\s+\d+\.|\Z)", content, re.DOTALL)
        if m6_match:
            m6_content = m6_match.group(1).strip()
            structure_parts = ["凤头", "猪肚", "豹尾"]
            for part in structure_parts:
                if part not in m6_content:
                    errors.append({
                        "rule": "B7",
                        "problem": f"章节结构中缺失【{part}】子模块",
                        "fix": f"在章节结构下补全‘### {part}’，描述该阶段的具体规划"
                    })

    # B8: M8 上下文准备及依赖文档存在性校验
    m8_match = re.search(r"##\s+8\.\s+上下文准备(.*?)(?=##\s+9\.|\Z)", content, re.DOTALL)
    if m8_match:
        m8_content = m8_match.group(1).strip()
        if "体系" not in m8_content and "引用" not in m8_content:
            errors.append({
                "rule": "B8",
                "problem": "模块 8 缺少【体系文档引用】或内容为空",
                "fix": "在上下文准备下添加具体引用的体系文档（如：修炼体系.md）"
            })
        else:
            # 物理文件存在性校验
            linked_files = re.findall(r"([\w\-_]+\.md)", m8_content)
            if project_root:
                for lf in linked_files:
                    if lf.endswith("blueprint.md") or lf.endswith("outline.md") or lf == "template.md":
                        continue
                    lf_path = os.path.join(project_root, lf)
                    char_lf_path = os.path.join(project_root, "人物体系", lf)
                    if not (os.path.exists(lf_path) or os.path.exists(char_lf_path)):
                        errors.append({
                            "rule": "B8",
                            "problem": f"模块 8 引用的文档在项目中不存在: “{lf}”",
                            "fix": f"请在项目根目录（或 人物体系/ 下）创建 {lf}，补齐设定"
                        })

    # B9: M9 章节结算输出非空校验
    m9_match = re.search(r"##\s+9\.\s+章节结算输出(.*?)(?:\Z)", content, re.DOTALL)
    if m9_match:
        m9_content = m9_match.group(1).strip()
        if len(m9_content) < 10:
            errors.append({
                "rule": "B9",
                "problem": "模块 9 章节结算输出内容过短",
                "fix": "请补充本章结束后核心道具变动、人物状态和暴露度变更记录"
            })
    else:
        # B1 已跑过 low 的过滤，在此若仍缺失且非 low 抛错
        if complexity != "low":
            errors.append({
                "rule": "B9",
                "problem": "缺失必要模块: “模块 9 · 章节结算输出”",
                "fix": "请在蓝图末尾以二级标题格式补齐 ## 9. 章节结算输出 并记录结算状态"
            })

    status = "FAIL" if errors else "PASS"
    return {
        "status": status,
        "errors": errors,
        "warnings": []
    }

def run_milestone_lint(file_path, project_root=None):
    if not os.path.exists(file_path):
        return {
            "status": "ERROR",
            "message": f"文件不存在: {file_path}",
            "errors": [],
            "warnings": []
        }

    with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
        content = f.read()

    errors = []

    # M1: 校验双大标题存在性
    has_a = re.search(r"^##\s+A\s*·?\s*(?:阶段成果|成果|Past)", content, re.MULTILINE | re.IGNORECASE) is not None
    has_b = re.search(r"^##\s+B\s*·?\s*(?:下一阶段入口|入口|Future)", content, re.MULTILINE | re.IGNORECASE) is not None
    
    if not has_a:
        errors.append({
            "rule": "M1",
            "problem": "里程碑缺失 A 节（## A · 阶段成果）主标题",
            "fix": "在里程碑文档中补齐：## A · 阶段成果（Past）"
        })
    if not has_b:
        errors.append({
            "rule": "M1",
            "problem": "里程碑缺失 B 节（## B · 下一阶段入口）主标题",
            "fix": "在里程碑文档中补齐：## B · 下一阶段入口（Future）"
        })

    # M2: A节四维度完备性 (人物状态, 资产, 地理时间, 四个解决)
    a_headers = [
        (re.compile(r"###\s*(?:人物状态|角色状态)", re.IGNORECASE), "人物状态"),
        (re.compile(r"###\s*(?:资产与能力清单|资产与能力|资产|清单)", re.IGNORECASE), "资产与能力清单"),
        (re.compile(r"###\s*(?:地理与时间|地理与时空|时空)", re.IGNORECASE), "地理与时间"),
        (re.compile(r"###\s*(?:本五章的\"四个解决\"|四个解决|四个\"解决\"|回顾四个问题)", re.IGNORECASE), "四个解决")
    ]
    for pattern, name in a_headers:
        if not pattern.search(content):
            errors.append({
                "rule": "M2",
                "problem": f"里程碑 A 节缺失二级或三级子版块: “{name}”",
                "fix": f"请在 A 节下以 ### {name} 格式补齐该版块"
            })

    # M3: B节下阶段入口实例化 (必须填充至少一项)
    b_headers = [
        "下一目标", "下一危机", "下一悬念", "下一去处", "下一抉择"
    ]
    b_match = re.search(r"##\s+B\s*·?\s*(?:下一阶段入口|入口|Future)(.*?)(?=##\s*钩子状态快照|\Z)", content, re.DOTALL | re.IGNORECASE)
    if b_match:
        b_content = b_match.group(1).strip()
        non_empty_count = 0
        for h in b_headers:
            h_match = re.search(r"###\s*" + re.escape(h) + r"(.*?)(?=###|\Z)", b_content, re.DOTALL)
            if h_match:
                h_text = h_match.group(1).strip()
                # 排除空白占位
                h_text_clean = re.sub(r"[\-\s\*\#]", "", h_text)
                if len(h_text_clean) > 5:
                    non_empty_count += 1
        if non_empty_count == 0:
            errors.append({
                "rule": "M3",
                "problem": "里程碑 B 节的所有入口子项（下一目标/危机/悬念/去处/抉择）均为空白或未填充",
                "fix": "请在 B 节中至少为一项提供具体的下阶段走向规划，避免断档"
            })
    else:
        errors.append({
            "rule": "M3",
            "problem": "未能解析里程碑 B 节（下一阶段入口）子项",
            "fix": "请确保 B 节格式正确"
        })

    # M4: 钩子快照物理依赖校验
    snap_match = re.search(r"##\s*钩子状态快照(.*?)(?=\Z)", content, re.DOTALL)
    if snap_match:
        snap_content = snap_match.group(1).strip()
        linked_hooks = re.findall(r"(hook-[\w\-]+\.md)", snap_content)
        if project_root:
            for hk in linked_hooks:
                hk_path = os.path.join(project_root, "plots", "active-hooks", hk)
                if not os.path.exists(hk_path):
                    errors.append({
                        "rule": "M4",
                        "problem": f"里程碑钩子快照表格引用了不存在的钩子文件: “{hk}”",
                        "fix": f"请检查 plots/active-hooks/ 下是否有该钩子文件，或修正快照表格中的文件名"
                    })

    status = "FAIL" if errors else "PASS"
    return {
        "status": status,
        "errors": errors,
        "warnings": []
    }

def run_hook_lint(file_path, project_root=None):
    if not os.path.exists(file_path):
        return {
            "status": "ERROR",
            "message": f"文件不存在: {file_path}",
            "errors": [],
            "warnings": []
        }

    with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
        content = f.read()

    errors = []

    # H1: 校验 Frontmatter 元数据
    fm_match = re.match(r"^---(.*?)---", content, re.DOTALL)
    if not fm_match:
        errors.append({
            "rule": "H1",
            "problem": "钩子追踪文档缺失 YAML Frontmatter 头部",
            "fix": "请在文件最上方添加 `---` 开关的 Frontmatter 描述"
        })
    else:
        fm_text = fm_match.group(1)
        type_match = re.search(r"type\s*:\s*(\w+)", fm_text)
        level_match = re.search(r"level\s*:\s*(\w+)", fm_text)
        status_match = re.search(r"status\s*:\s*(\w+)", fm_text)
        
        if not type_match or type_match.group(1).strip() != "plot_hook":
            errors.append({
                "rule": "H1",
                "problem": "钩子 YAML 属性 type 不合法，必须为: type: plot_hook",
                "fix": "在 Frontmatter 中设置 type: plot_hook"
            })
        if not level_match or level_match.group(1).strip() not in ["short_term", "mid_term", "long_term"]:
            errors.append({
                "rule": "H1",
                "problem": f"钩子 YAML 属性 level 不合法，必须为 short_term/mid_term/long_term 之一",
                "fix": "在 Frontmatter 中设置 level 为 short_term、mid_term 或 long_term 之一"
            })
        if not status_match or status_match.group(1).strip() not in ["Seed", "Active", "Revealing", "Resolved"]:
            errors.append({
                "rule": "H1",
                "problem": f"钩子 YAML 属性 status 不合法，必须为 Seed/Active/Revealing/Resolved 之一",
                "fix": "在 Frontmatter 中设置 status 为 Seed、Active、Revealing 或 Resolved 之一"
            })

    # H2: 双节结构完整性
    has_public = re.search(r"^##\s+1\.\s+读者已知线索", content, re.MULTILINE) is not None
    has_hidden = re.search(r"^##\s+2\.\s+创作者大纲占位", content, re.MULTILINE) is not None
    
    if not has_public:
        errors.append({
            "rule": "H2",
            "problem": "剧情钩子缺失公共展示区主标题（## 1. 读者已知线索 (Public)）",
            "fix": "在钩子文档中添加：## 1. 读者已知线索 (Public)"
        })
    if not has_hidden:
        errors.append({
            "rule": "H2",
            "problem": "剧情钩子缺失上帝大纲占位区主标题（## 2. 创作者大纲占位 (Hidden)）",
            "fix": "在钩子文档中添加：## 2. 创作者大纲占位 (Hidden)"
        })

    # H3: 已知线索非空检查 (若状态不是 Seed)
    if fm_match:
        fm_text = fm_match.group(1)
        status_match = re.search(r"status\s*:\s*(\w+)", fm_text)
        status = status_match.group(1).strip() if status_match else "Seed"
        if status != "Seed":
            public_match = re.search(r"##\s+1\.\s+读者已知线索(.*?)(?=##\s+2\.|\Z)", content, re.DOTALL)
            if public_match:
                public_content = public_match.group(1).strip()
                public_clean = re.sub(r"[\-\s\*\#\>【】对正文可见]", "", public_content)
                if len(public_clean) < 10 or "已掉落" in public_clean:
                    errors.append({
                        "rule": "H3",
                        "problem": f"当前钩子处于激活状态 ({status})，但【读者已知线索】为空或只含模板文字",
                        "fix": f"在 ## 1. 读者已知线索 下补充本章或以前已为读者所知的线索"
                    })

    status = "FAIL" if errors else "PASS"
    return {
        "status": status,
        "errors": errors,
        "warnings": []
    }

def generate_blueprint_markdown_report(result, vol, ch):
    score = 10 - min(10, len(result["errors"]))
    status_str = "PASS" if result["status"] == "PASS" else "FAIL"

    report = []
    report.append(f"# 章节蓝图完备性检查报告 · 卷 {vol} · 第 {ch} 章\n")
    report.append("## 总览\n")
    report.append(f"- **总分**：{score}/10")
    report.append(f"- **蓝图校验结果**：{status_str}\n")

    report.append("## 完备性检查清单 (B1-B8)\n")
    report.append("| 检查项 | 状态 | 问题 | 建议 |")
    report.append("| --- | --- | --- | --- |")

    b_rules = {
        "B1": "8个模块齐备", "B2": "叙事目标合理性", "B3": "情绪节奏点数量",
        "B4": "场景设计合理性", "B5": "叙事重点存在性", "B7": "章节结构三段式",
        "B8": "上下文引用清单"
    }

    rule_status = {k: "PASS" for k in b_rules.keys()}
    for err in result["errors"]:
        rule_status[err["rule"]] = "FAIL"

    for b_code, b_name in b_rules.items():
        if rule_status[b_code] == "PASS":
            report.append(f"| {b_name} | PASS | - | - |")
        else:
            for err in result["errors"]:
                if err["rule"] == b_code:
                    report.append(f"| {b_name} | FAIL | **问题**: {err['problem']} | {err['fix']} |")

    report.append("\n## 结论\n")
    if result["status"] == "PASS":
        report.append("- [x] 蓝图校验 PASS → 可以进入章节正文创作阶段")
    else:
        report.append("- [ ] 蓝图校验 FAIL → 蓝图不合格，请根据上述清单修改蓝图后再交付")

    return "\n".join(report)

def find_and_load_project_config(start_path):
    curr = os.path.abspath(start_path)
    if os.path.isfile(curr):
        curr = os.path.dirname(curr)

    config_names = ["novel-project.md", "项目红线.md", "CODEBUDDY.md", "CLAUDE.md"]
    config_path = None

    while True:
        for name in config_names:
            p = os.path.join(curr, name)
            if os.path.exists(p):
                config_path = p
                break
        if config_path:
            break
        parent = os.path.dirname(curr)
        if parent == curr: # root reached
            break
        curr = parent

    if not config_path:
        return {
            "status": "NOT_FOUND",
            "project_root": os.getcwd(),
            "genre": "修真",
            "custom_banned_words": [],
            "idiom_density_min": 8.0,
            "idiom_density_max": 22.0,
            "daoist_density_min": 15.0,
            "daoist_density_max": 40.0,
            "bystander_ratio_min": 0.15
        }

    project_root = os.path.dirname(config_path)

    try:
        with open(config_path, "r", encoding="utf-8", errors="ignore") as f:
            content = f.read()
    except Exception as e:
        return {
            "status": "ERROR",
            "message": str(e),
            "project_root": project_root,
            "genre": "修真",
            "custom_banned_words": [],
            "idiom_density_min": 8.0,
            "idiom_density_max": 22.0,
            "daoist_density_min": 15.0,
            "daoist_density_max": 40.0,
            "bystander_ratio_min": 0.15
        }

    # 解析题材与元数据
    genre = "修真"
    meta_match = re.search(r"##\s+项目元信息(.*?)(?=##|\Z)", content, re.DOTALL)
    if meta_match:
        meta_text = meta_match.group(1)
        for line in meta_text.split("\n"):
            line = line.strip()
            if "题材" in line:
                val = re.split(r"[：:]", line, 1)
                if len(val) > 1:
                    genre = val[1].strip(" -*")
            elif "项目根目录" in line:
                val = re.split(r"[：:]", line, 1)
                if len(val) > 1:
                    custom_root = val[1].strip(" -*")
                    if os.path.isabs(custom_root):
                        project_root = custom_root
                    else:
                        project_root = os.path.abspath(os.path.join(os.path.dirname(config_path), custom_root))

    # 解析禁用词追加
    custom_banned_words = []
    match = re.search(r"##\s+禁用词追加(.*?)(?=##|\Z)", content, re.DOTALL)
    if match:
        section_text = match.group(1)
        for line in section_text.split("\n"):
            line = line.strip()
            if line.startswith(("-", "*")):
                item = line[1:].strip()
                if ":" in item:
                    item = item.split(":", 1)[1].strip()
                elif "：" in item:
                    item = item.split("：", 1)[1].strip()
                for w in re.split(r"[、，,；;\s]+", item):
                    w = w.strip("“\"'” ")
                    if w:
                        custom_banned_words.append(w)

    # 解析笔力指标配置
    idiom_density_min = 8.0
    idiom_density_max = 22.0
    daoist_density_min = 15.0
    daoist_density_max = 40.0
    bystander_ratio_min = 0.15

    style_match = re.search(r"##\s+(?:笔力指标配置|笔力特征指标)(.*?)(?=##|\Z)", content, re.DOTALL)
    if style_match:
        style_text = style_match.group(1)
        for line in style_text.split("\n"):
            line = line.strip()
            if not line:
                continue
            val_parts = re.split(r"[：:]", line, 1)
            if len(val_parts) > 1:
                key = val_parts[0].strip(" -*")
                val_str = val_parts[1].strip(" -*%")
                try:
                    val_num = float(val_str)
                    if "%" in line:
                        val_num = val_num / 100.0 if val_num > 1.0 else val_num
                    if "四字成语密度下限" in key or "idiom_density_min" in key:
                        idiom_density_min = val_num
                    elif "四字成语密度上限" in key or "idiom_density_max" in key:
                        idiom_density_max = val_num
                    elif "道家词汇密度下限" in key or "daoist_density_min" in key or "古风词汇密度下限" in key:
                        daoist_density_min = val_num
                    elif "道家词汇密度上限" in key or "daoist_density_max" in key or "古风词汇密度上限" in key:
                        daoist_density_max = val_num
                    elif "侧面烘托比率下限" in key or "bystander_ratio_min" in key or "侧面烘托占比下限" in key:
                        bystander_ratio_min = val_num if "%" not in line and val_num < 1.0 else (val_num / 100.0 if val_num > 1.0 else val_num)
                except ValueError:
                    pass

    return {
        "status": "FOUND",
        "config_path": config_path,
        "project_root": project_root,
        "genre": genre,
        "custom_banned_words": custom_banned_words,
        "idiom_density_min": idiom_density_min,
        "idiom_density_max": idiom_density_max,
        "daoist_density_min": daoist_density_min,
        "daoist_density_max": daoist_density_max,
        "bystander_ratio_min": bystander_ratio_min
    }

def read_markdown_section(file_path, anchor):
    import urllib.parse
    try:
        with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
            lines = f.readlines()
    except Exception as e:
        print(f"无法读取文件: {file_path} (错误: {e})", file=sys.stderr)
        return None

    anchor_clean = re.sub(r'[\s\-_\#\.]', '', urllib.parse.unquote(anchor).strip().lower())
    
    found = False
    target_level = 0
    section_lines = []
    
    for line in lines:
        line_stripped = line.strip()
        if line_stripped.startswith("#"):
            m = re.match(r"^(#+)", line_stripped)
            level = len(m.group(1)) if m else 0
            title_text = line_stripped.lstrip("#").strip().lower()
            title_clean = re.sub(r'[\s\-_\#\.]', '', title_text)
            
            if found:
                if level <= target_level:
                    break
                else:
                    section_lines.append(line)
            else:
                if title_clean == anchor_clean:
                    found = True
                    target_level = level
                    section_lines.append(line)
        else:
            if found:
                section_lines.append(line)
                
    if not found:
        return None
        
    return "".join(section_lines)

def check_project_integrity(project_root):
    errors = []
    chapters = []
    
    # 0. 基础目录与总大纲校验
    contents_dir = os.path.join(project_root, "contents")
    if not os.path.exists(contents_dir):
        return {
            "status": "FAIL",
            "errors": [{
                "rule": "CI_DIR",
                "problem": f"缺失核心正文目录: {contents_dir}",
                "fix": "请在项目根目录下创建 contents 文件夹以存放正文章节"
            }],
            "chapter_count": 0
        }
        
    master_outline_rel = os.path.join("outlines", "总大纲.md")
    master_outline_path = os.path.join(project_root, master_outline_rel)
    if not os.path.exists(master_outline_path):
        errors.append({
            "rule": "CI_MASTER_OUTLINE_MISSING",
            "problem": f"缺失全书总大纲: {master_outline_rel}",
            "fix": "请在 outlines/ 目录下创建 总大纲.md，并按照 references/master-outline-template.md 结构进行规划"
        })
    else:
        try:
            with open(master_outline_path, "r", encoding="utf-8", errors="ignore") as f:
                content = f.read()
            if "type: master_outline" not in content and "master_outline" not in content:
                errors.append({
                    "rule": "CI_MASTER_OUTLINE_FORMAT",
                    "problem": f"总大纲格式不合规: {master_outline_rel}",
                    "fix": "确保 总大纲.md 头部包含 type: master_outline 且具有主线脉络与卷概要等核心版块"
                })
        except Exception as e:
            errors.append({
                "rule": "CI_MASTER_OUTLINE_READ",
                "problem": f"无法读取总大纲: {master_outline_rel} (错误: {str(e)})",
                "fix": "请检查该文件状态与读取权限"
            })

    # 1. 扫描正文章节并提取卷号与章号
    vols = set()
    for root, dirs, files in os.walk(contents_dir):
        for f in files:
            if f.endswith(".txt"):
                file_path = os.path.join(root, f)
                vol_match = re.search(r"(?:volume[-_]|v)(\d+)", file_path, re.IGNORECASE)
                ch_match = re.search(r"(?:chapter[-_]|ch)(\d+)", file_path, re.IGNORECASE)
                if vol_match or ch_match:
                    vol_str = vol_match.group(1) if vol_match else "1"
                    ch_str = ch_match.group(1) if ch_match else "1"
                    vol = int(vol_str)
                    ch = int(ch_str)
                    chapters.append((vol, ch, file_path, vol_str, ch_str))
                    vols.add((vol, vol_str))
                    
    chapters.sort(key=lambda x: (x[0], x[1]))
    
    # 2. 卷大纲存在性与格式校验
    for vol, vol_str in vols:
        # 支持多种可能的命名样式（volume-1-outline.md, volume-01-outline.md, v01-outline.md, v1-outline.md）
        vol_outline_candidates = [
            os.path.join("outlines", f"volume-{vol}-outline.md"),
            os.path.join("outlines", f"volume-{vol_str}-outline.md"),
            os.path.join("outlines", f"v{vol}-outline.md"),
            os.path.join("outlines", f"v{vol_str}-outline.md"),
        ]
        vol_outline_path = None
        for candidate in vol_outline_candidates:
            p = os.path.join(project_root, candidate)
            if os.path.exists(p):
                vol_outline_path = p
                break
                
        if not vol_outline_path:
            errors.append({
                "rule": "CI_VOLUME_OUTLINE_MISSING",
                "problem": f"缺失卷 {vol} 大纲 (尝试查找: {', '.join(vol_outline_candidates)})",
                "fix": f"请在 outlines/ 目录下创建 volume-{vol}-outline.md，并按照 references/volume-outline-template.md 结构进行规划"
            })
        else:
            try:
                with open(vol_outline_path, "r", encoding="utf-8", errors="ignore") as f:
                    content = f.read()
                # 检查大纲中的核心部分，支持简化的包含校验
                headers = ["核心设计", "Arc", "人物动态", "冲突", "钩子", "依赖声明"]
                missing_headers = []
                for h in headers:
                    if h not in content:
                        missing_headers.append(h)
                if missing_headers:
                    errors.append({
                        "rule": "CI_VOLUME_OUTLINE_FORMAT",
                        "problem": f"卷 {vol} 大纲缺少必要版块: {os.path.relpath(vol_outline_path, project_root)}，缺少的内容包括: {', '.join(missing_headers)}",
                        "fix": f"请编辑该大纲，补齐缺少的大纲版块"
                    })
            except Exception as e:
                errors.append({
                    "rule": "CI_VOLUME_OUTLINE_READ",
                    "problem": f"无法读取卷大纲: {os.path.relpath(vol_outline_path, project_root)} (错误: {str(e)})",
                    "fix": "请检查该文件状态与读取权限"
                })

    # 3. 逐章依赖与完整性静态分析 (章节蓝图、出场人物依赖 M2、设定百科依赖 M8)
    for vol, ch, file_path, vol_str, ch_str in chapters:
        # A. 章节蓝图文件检查
        blueprint_candidates = [
            os.path.join("blueprints", f"volume-{vol}", f"chapter-{ch_str}-blueprint.md"),
            os.path.join("blueprints", f"volume-{vol}", f"chapter-{ch}-blueprint.md"),
            os.path.join("blueprints", f"v{vol_str}", f"ch{ch}-blueprint.md"),
            os.path.join("blueprints", f"v{vol_str}", f"ch{ch_str}-blueprint.md"),
            os.path.join("blueprints", f"volume-{vol_str}", f"chapter-{ch_str}-blueprint.md"),
        ]
        blueprint_path = None
        for candidate in blueprint_candidates:
            p = os.path.join(project_root, candidate)
            if os.path.exists(p):
                blueprint_path = p
                break
                
        if not blueprint_path:
            errors.append({
                "rule": "CI_BLUEPRINT",
                "problem": f"缺失对应章节蓝图 (尝试查找: {', '.join(blueprint_candidates)}) (对应正文: {os.path.join('contents', f'volume-{vol}', os.path.basename(file_path))})",
                "fix": f"请使用 plan 阶段在该路径下生成章节完备的蓝图文件"
            })
        else:
            try:
                with open(blueprint_path, "r", encoding="utf-8", errors="ignore") as bf:
                    b_content = bf.read()
                
                # 校验 M2 声明的出场角色依赖
                characters = set()
                for line in b_content.splitlines():
                    if "出场角色" in line or "出场人物" in line:
                        parts = re.split(r"：|:", line, 1)
                        if len(parts) > 1:
                            names = re.split(r"[、，,；;\s/]+", parts[1].strip())
                            for name in names:
                                name = name.strip("“\"'” *-_#`[]()")
                                if name and name not in ["出场角色", "出场人物", "角色", "无", "待定", "出场角色"]:
                                    characters.add(name)
                
                for char in characters:
                    char_path = os.path.join(project_root, "人物体系", f"{char}.md")
                    if not os.path.exists(char_path):
                        errors.append({
                            "rule": "CI_CHARACTER_MISSING",
                            "problem": f"章节 {ch} 蓝图引用了未定义的角色: {char} (在 {os.path.relpath(blueprint_path, project_root)} 中声明)",
                            "fix": f"请在 人物体系/ 目录下创建 {char}.md 文件，补齐该角色的静态设定档案"
                        })
                
                # 校验 M8 声明的设定百科依赖
                system_docs = set()
                m8_match = re.search(r"(?:模块 8|上下文准备)(.*)", b_content, re.DOTALL | re.IGNORECASE)
                m8_text = m8_match.group(1) if m8_match else b_content
                
                matches = re.findall(r"([\w\-_]+\.md)", m8_text)
                for m in matches:
                    if m.endswith("blueprint.md") or m.endswith("outline.md") or m == "template.md":
                        continue
                    system_docs.add(m)
                
                for s_doc in system_docs:
                    s_doc_path = os.path.join(project_root, s_doc)
                    if not os.path.exists(s_doc_path):
                        # 也可能是指向人物体系下的档案
                        char_doc_path = os.path.join(project_root, "人物体系", s_doc)
                        if not os.path.exists(char_doc_path):
                            errors.append({
                                "rule": "CI_SYSTEM_DOC_MISSING",
                                "problem": f"章节 {ch} 蓝图引用了不存在的设定/百科文档: {s_doc} (在 {os.path.relpath(blueprint_path, project_root)} 模块 8 中声明)",
                                "fix": f"请在项目根目录下（或 人物体系/ 下）创建 {s_doc}，补齐相关设定"
                            })
            except Exception as e:
                errors.append({
                    "rule": "CI_BLUEPRINT_READ",
                    "problem": f"无法读取蓝图文件: {os.path.relpath(blueprint_path, project_root)} (错误: {str(e)})",
                    "fix": "请检查该文件状态与读取权限"
                })
            
        # B. 里程碑阶段归档检查
        if ch % 5 == 0:
            milestone_candidates = [
                os.path.join("plots", "milestones", f"vol-{vol}-ch-{ch}-summary.md"),
                os.path.join("plots", "milestones", f"vol-{vol}-ch-{ch_str}-summary.md"),
                os.path.join("plots", "milestones", f"vol-{vol_str}-ch-{ch_str}-summary.md"),
            ]
            milestone_path = None
            for candidate in milestone_candidates:
                p = os.path.join(project_root, candidate)
                if os.path.exists(p):
                    milestone_path = p
                    break
                    
            if not milestone_path:
                errors.append({
                    "rule": "CI_MILESTONE",
                    "problem": f"缺失第 {ch} 章对应的里程碑文件 (尝试查找: {', '.join(milestone_candidates)})",
                    "fix": f"请在该路径下新建里程碑总结，补齐 A、B 双节信息"
                })
            else:
                try:
                    with open(milestone_path, "r", encoding="utf-8", errors="ignore") as mf:
                        m_content = mf.read()
                    has_a = re.search(r"A\s*节|阶段成果", m_content, re.IGNORECASE) is not None
                    has_b = re.search(r"B\s*节|下一阶段入口", m_content, re.IGNORECASE) is not None
                    if not (has_a and has_b):
                        errors.append({
                            "rule": "CI_MILESTONE_FORMAT",
                            "problem": f"里程碑格式缺失 B 节或 A 节: {os.path.relpath(milestone_path, project_root)}",
                            "fix": "打开文件，补充 A 节（阶段成果）及 B 节（下一阶段入口）标题与描述内容"
                        })
                except Exception as e:
                    errors.append({
                        "rule": "CI_MILESTONE_READ",
                        "problem": f"无法读取里程碑文件: {os.path.relpath(milestone_path, project_root)} (错误: {str(e)})",
                        "fix": "请检查该文件状态与读取权限"
                    })

    # 4. 扫描活跃钩子
    hooks_dir = os.path.join(project_root, "plots", "active-hooks")
    if os.path.exists(hooks_dir):
        for f in os.listdir(hooks_dir):
            if f.endswith(".md"):
                hook_path = os.path.join(hooks_dir, f)
                if os.path.getsize(hook_path) < 10:
                    errors.append({
                        "rule": "CI_HOOK",
                        "problem": f"空钩子跟踪文档: plots/active-hooks/{f}",
                        "fix": "在此钩子追踪文件内填入引入章节、核心悬念、线索掉落等内容"
                    })

    # 5. 校验项目内所有 markdown 相对链接与锚点完整性
    for root_dir_walk, dirs, files in os.walk(project_root):
        dirs[:] = [d for d in dirs if not d.startswith('.') and d not in ('node_modules', 'logs')]
        for f in files:
            if f.endswith(".md"):
                md_path = os.path.join(root_dir_walk, f)
                try:
                    with open(md_path, "r", encoding="utf-8", errors="ignore") as file:
                        md_content = file.read()
                except Exception as e:
                    errors.append({
                        "rule": "CI_MD_READ_ERROR",
                        "problem": f"无法读取 markdown 文件: {os.path.relpath(md_path, project_root)} (错误: {str(e)})",
                        "fix": "请检查该文件状态与读取权限"
                    })
                    continue
                
                link_pattern = re.compile(r'\[([^\]]+)\]\((?!https?://|mailto:)([^)]+)\)')
                for m in link_pattern.finditer(md_content):
                    link_url = m.group(2).strip()
                    if not link_url:
                        continue
                    if link_url.startswith("#"):
                        target_file_path = md_path
                        anchor = link_url[1:]
                    else:
                        if "#" in link_url:
                            rel_path, anchor = link_url.split("#", 1)
                        else:
                            rel_path = link_url
                            anchor = None
                        target_file_path = os.path.normpath(os.path.join(root_dir_walk, rel_path))
                    if not os.path.exists(target_file_path):
                        errors.append({
                            "rule": "CI_BROKEN_LINK",
                            "problem": f"文件 {os.path.relpath(md_path, project_root)} 中的链接指向不存在的文件: {link_url}",
                            "fix": f"请检查该链接路径是否正确，或创建对应的文件: {os.path.relpath(target_file_path, project_root)}"
                        })
                    elif anchor:
                        import urllib.parse
                        anchor_decoded = urllib.parse.unquote(anchor).strip().lower()
                        anchor_found = False
                        try:
                            with open(target_file_path, "r", encoding="utf-8", errors="ignore") as tf:
                                t_lines = tf.readlines()
                            for tl in t_lines:
                                tl = tl.strip()
                                if tl.startswith("#"):
                                    title_text = tl.lstrip("#").strip().lower()
                                    t_clean = re.sub(r'[\s\-_\#\.]', '', title_text)
                                    a_clean = re.sub(r'[\s\-_\#\.]', '', anchor_decoded)
                                    if t_clean == a_clean:
                                        anchor_found = True
                                        break
                        except Exception:
                            pass
                        if not anchor_found:
                            errors.append({
                                "rule": "CI_BROKEN_ANCHOR",
                                "problem": f"文件 {os.path.relpath(md_path, project_root)} 中的链接锚点在目标文件中不存在: {link_url}",
                                "fix": f"请在目标文件 {os.path.relpath(target_file_path, project_root)} 中添加对应的标题（如 # {anchor_decoded}），或修正该链接中的锚点名称"
                            })

    status = "FAIL" if errors else "PASS"
    return {
        "status": status,
        "errors": errors,
        "chapter_count": len(chapters)
    }

def main():
    parser = argparse.ArgumentParser(description="Novel Quality Linter (Harness Engine)")
    parser.add_argument("file_path", nargs="?", default="", help="要检查的小说文件 (.txt 正文 或 .md 蓝图) 路径")
    parser.add_argument("--vol", type=int, default=1, help="卷号")
    parser.add_argument("--ch", type=int, default=1, help="章号")
    parser.add_argument("--report-dir", default="", help="保存 Markdown 报告 of 目录路径")
    parser.add_argument("--json", action="store_true", help="仅输出 JSON 结果到终端")
    parser.add_argument("--check-project", action="store_true", help="对整个项目工作区进行完整性与对齐扫描门禁")
    parser.add_argument("--read-section", default="", help="按需提取并输出指定 Markdown 文件中的指定标题片段，格式: 'path.md#header'")

    args = parser.parse_args()

    # 自动发现并加载项目配置
    start_path = args.file_path if args.file_path else os.getcwd()
    config = find_and_load_project_config(start_path)

    # 如果是读取片段
    if args.read_section:
        if "#" not in args.read_section:
            print("错误: --read-section 参数格式错误，必须为 'path.md#header' 格式。")
            sys.exit(1)
        
        file_path_part, anchor_part = args.read_section.split("#", 1)
        real_file_path = file_path_part
        if not os.path.exists(real_file_path):
            real_file_path = os.path.join(config["project_root"], file_path_part)
            if not os.path.exists(real_file_path):
                print(f"错误: 找不到指定的文件: {file_path_part} (也尝试过 {real_file_path})")
                sys.exit(1)
                
        section_content = read_markdown_section(real_file_path, anchor_part)
        if section_content is None:
            print(f"错误: 在文件 {file_path_part} 中未找到对应的标题锚点: {anchor_part}")
            sys.exit(1)
            
        print(section_content)
        sys.exit(0)

    # 如果是检查整个项目完整性
    if args.check_project:
        result = check_project_integrity(config["project_root"])
        if args.json:
            print(json.dumps(result, ensure_ascii=False, indent=2))
            sys.exit(1 if result["status"] == "FAIL" else 0)

        print("=" * 60)
        print(f"项目完整性检测 (CI Scan): {config['project_root']}")
        print(f"项目元配置加载: {config['status']} ({config.get('config_path', 'N/A')})")
        print(f"题材元数据: {config['genre']}")
        print(f"检测正文章节数: {result['chapter_count']} 章")
        print(f"完备性状态: {result['status']}")
        print(f"红线对齐错误: {len(result['errors'])} 个")
        print("=" * 60)

        if result["errors"]:
            print("\n[项目完整性拦截 FAIL 清单]")
            for err in result["errors"]:
                print(f" -> [{err['rule']}] 问题: {err['problem']}")
                print(f"    建议: {err['fix']}\n")
        else:
            print("\n恭喜，项目完整性、对齐与归档校验 100% 通过！\n")

        sys.exit(1 if result["status"] == "FAIL" else 0)

    # 正常的文件校验必须指定路径
    if not args.file_path:
        parser.print_help()
        sys.exit(1)

    # 自动从路径中解析卷号和章号
    vol_match = re.search(r"volume[-_](\d+)", args.file_path, re.IGNORECASE)
    ch_match = re.search(r"chapter[-_](\d+)", args.file_path, re.IGNORECASE)
    vol = int(vol_match.group(1)) if vol_match else args.vol
    ch = int(ch_match.group(1)) if ch_match else args.ch

    # 检测文件类型元数据，实现智能路由
    doc_type = "txt"
    if args.file_path.endswith(".md"):
        doc_type = "markdown"
        try:
            with open(args.file_path, "r", encoding="utf-8", errors="ignore") as f:
                head = f.read(1000)
            fm_m = re.match(r"^---(.*?)---", head, re.DOTALL)
            if fm_m:
                fm_text = fm_m.group(1)
                type_match = re.search(r"type\s*:\s*(\w+)", fm_text)
                if type_match:
                    doc_type = type_match.group(1).strip()
        except Exception:
            pass

    is_blueprint = (doc_type == "chapter_blueprint" or "blueprint" in os.path.basename(args.file_path).lower())
    is_milestone = (doc_type == "milestone_summary" or "summary" in os.path.basename(args.file_path).lower())
    is_hook = (doc_type == "plot_hook" or "hook" in os.path.basename(args.file_path).lower())

    trigger_flags = []

    if is_blueprint:
        result = run_blueprint_lint(args.file_path, project_root=config["project_root"])
    elif is_milestone:
        result = run_milestone_lint(args.file_path, project_root=config["project_root"])
    elif is_hook:
        result = run_hook_lint(args.file_path, project_root=config["project_root"])
    else:
        # 正文章节检测，尝试加载本章蓝图中的 M7 触发标记以检测 T4
        blueprint_candidates = [
            os.path.join(config["project_root"], "blueprints", f"volume-{vol}", f"chapter-{ch}-blueprint.md"),
            os.path.join(config["project_root"], "blueprints", f"volume-{vol}", f"chapter-{ch:02d}-blueprint.md"),
            os.path.join(config["project_root"], "blueprints", f"volume-{vol}", f"chapter_{ch}-blueprint.md"),
        ]
        for candidate in blueprint_candidates:
            if os.path.exists(candidate):
                try:
                    with open(candidate, "r", encoding="utf-8", errors="ignore") as bf:
                        b_content = bf.read()
                    # 检查大悲 / 大怒 / 大惧 触发标记是否勾选 [x]
                    if re.search(r"\[x\]\s*(?:大悲\s*/\s*大怒\s*/\s*大惧|情绪爆发)", b_content, re.IGNORECASE):
                        trigger_flags.append('T4')
                    # 检查战斗触发标记是否勾选 [x]
                    if re.search(r"\[x\]\s*(?:战斗|打斗|对决|斗法|交手)", b_content, re.IGNORECASE):
                        trigger_flags.append('T1')
                except Exception:
                    pass
                break

        result = run_lint(
            args.file_path,
            project_root=config["project_root"],
            custom_banned_words=config["custom_banned_words"],
            trigger_flags=trigger_flags,
            config=config
        )

    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
        sys.exit(1 if result["status"] == "FAIL" else 0)

    # 打印简要控制台信息
    print("=" * 60)
    print(f"检测文件: {args.file_path} (卷 {vol} · 第 {ch} 章)")
    print(f"项目配置: {config['status']} ({config.get('config_path', 'N/A')})")
    doc_type_desc = "正文章节"
    if is_blueprint: doc_type_desc = "章节蓝图"
    elif is_milestone: doc_type_desc = "里程碑总结"
    elif is_hook: doc_type_desc = "剧情钩子"
    print(f"检测类型: {doc_type_desc}")
    print(f"检测状态: {result['status']}")
    print(f"红线错误: {len(result['errors'])} 个 | 黄线预警: {len(result['warnings'])} 个")
    print("=" * 60)

    if not is_blueprint and not is_milestone and not is_hook:
        print("笔力指标统计:")
        print(f"  - 四字成语密度: {result.get('idiom_density', 0.0):.2f}/千字 (共 {result.get('idiom_count', 0)} 个)")
        print(f"  - 易经道德经/古风词密度: {result.get('daoist_density', 0.0):.2f}/千字 (共 {result.get('daoist_count', 0)} 个)")
        if 'T1' in trigger_flags:
            print(f"  - 侧面烘托比率: {result.get('bystander_ratio', 0.0)*100:.1f}% (动作 {result.get('action_count', 0)} 段, 反应 {result.get('reaction_count', 0)} 段)")
        print("=" * 60)

    if result["errors"]:
        print(f"\n[{doc_type_desc}拦截 FAIL 清单]")
        for err in result["errors"]:
            if is_blueprint or is_milestone or is_hook:
                print(f" -> [{err['rule']}] 问题: {err['problem']}")
            else:
                loc = f"第 {err['line']} 行" if err["line"] > 0 else "章节级"
                print(f" -> [{loc}] [{err['rule']}] 问题: {err['problem']}")
                if err["content"]:
                    print(f"    上下文: ... {err['content']} ...")
            print(f"    建议: {err['fix']}\n")

    if result["warnings"]:
        print("\n[黄线预警 Y6 清单]")
        for warn in result["warnings"]:
            print(f" -> [行 {warn['line']}] [{warn['rule']}] 问题: {warn['problem']}")
            print(f"    建议: {warn['fix']}\n")

    # 生成并写入 Markdown 报告
    if args.report_dir:
        os.makedirs(args.report_dir, exist_ok=True)
        if is_blueprint:
            report_md = generate_blueprint_markdown_report(result, vol, ch)
            report_name = f"vol-{vol}-ch-{ch}-blueprint-report.md"
        elif is_milestone:
            report_md = f"# 里程碑完备性报告\n\n状态: {result['status']}\n错误数: {len(result['errors'])}"
            report_name = f"vol-{vol}-ch-{ch}-milestone-report.md"
        elif is_hook:
            report_md = f"# 剧情钩子完备性报告\n\n状态: {result['status']}\n错误数: {len(result['errors'])}"
            report_name = f"hook-report.md"
        else:
            report_md = generate_markdown_report(result, vol, ch)
            report_name = f"vol-{vol}-ch-{ch}-quality-report.md"
            
        report_path = os.path.join(args.report_dir, report_name)
        with open(report_path, "w", encoding="utf-8") as f:
            f.write(report_md)
        print(f"已自动生成报告: {report_path}")

    sys.exit(1 if result["status"] == "FAIL" else 0)

if __name__ == "__main__":
    main()