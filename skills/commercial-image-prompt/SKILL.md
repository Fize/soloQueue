---
name: commercial-image-prompt
description: "Commercial and e-commerce image generation methodology — three-layer prompt engineering framework (subject-scene / style-texture / technical-constraints), scenario-based template library, and iterative optimization strategies for Chinese e-commerce and social media platforms."
when_to_use: "Trigger when the user mentions: 生图, 电商主图, 海报, 营销图, 封面图, 提示词, prompt engineering, image generation, commercial image, e-commerce image"
---

# Commercial Image Prompt — Three-Layer Prompting Methodology

Designed for Chinese e-commerce platforms (Taobao, JD, Pinduoduo) and WeChat ecosystem (public accounts, Xiaohongshu, Douyin). Core approach: speak naturally, use metaphors, leverage Chinese-aesthetic tags.

---

## Core Methodology: Three-Layer Prompting (三层描述法)

### Layer 1: Subject & Scene Setting
A single sentence describing the core image, as if telling a friend what you want to see. Focus on subject, scene, lighting, and the single most important element.

**Pattern**: "Generate a professional e-commerce product image of [product] in [scene], lit by [lighting type], emphasizing [key selling point]."

### Layer 2: Style & Texture Enhancement
2-3 specific style tags + reference styles. Define the visual mood, texture, and artistic reference. Use Chinese aesthetic keywords where appropriate.

**Pattern**: "Style referencing [brand style / artist], [color mood description], [texture quality]."

**Chinese aesthetic tags**: 国潮风 (China-chic), 新中式 (neo-Chinese), 复古港风 (HK retro), 赛博朋克 (cyberpunk), 水墨意境 (ink-wash), 极简留白 (minimal white-space).

### Layer 3: Technical Constraints & Negative Prompts
Platform-specific dimensions, critical element positioning, and explicit prohibitions.

**Pattern**: "Ratio [W:H]. [Color] background. Product occupies [X]% of frame center. NO text, NO watermarks, NO silhouettes."

---

## Scenario-Based Prompt Templates

### Template 1: E-Commerce Product Main Image (White Background / Scene)
```
Layer 1: Generate a professional e-commerce product image of [product] in [scene].
Lighting [type], highlighting [key feature].
Layer 2: Style reference: [brand style], [color tone].
Layer 3: Ratio [W:H], [color] background. Product occupies center ~[X]% of frame.
No text, no watermarks, no reflections or silhouettes.
```

### Template 2: Promotional Campaign Poster (Holiday / Flash Sale)
```
Layer 1: Generate a [campaign name] opening poster. Visual center is [core element].
Surrounded by [atmosphere elements], background [color direction].
Layer 2: Style: [platform] campaign visual style, evoking [emotion/feeling].
Layer 3: Horizontal composition, ratio [W:H]. Reserve [X]% at [position] for text overlay.
```

### Template 3: WeChat Public Account Cover Image
```
Layer 1: Generate an illustration for [article topic]. Center is [scene description],
composition [layout style].
Layer 2: Style: knowledge account [style description], color palette [scheme].
Layer 3: Vertical composition, ratio 2.35:1. Key elements concentrated in lower 2/3.
Upper 1/3 reserved for title text overlay.
```

### Template 4: Xiaohongshu / Douyin Cover
```
Layer 1: Generate a [content topic] cover image. Scene is [scene description],
overall feeling [emotion keywords].
Layer 2: Style: Xiaohongshu popular [category] blogger cover style. High saturation,
high contrast, "internet-native" visual feel.
Layer 3: Ratio 3:4 (vertical). Text zones at top and bottom. Center area has clean
unobstructed imagery. Colors must be eye-catching and punchy.
```

---

## Iteration & Optimization Strategies

### Micro-Adjustments
- **Clean up background**: "Good overall, but make the background cleaner / more lively"
- **Element swap**: "Keep the subject, replace [element A] with [element B] beside it"
- **Style transfer**: "Keep the composition, change the style from [A] to [B]"
- **Ratio correction**: "Keep content, regenerate in vertical (9:16) format"
- **Detail enhancement**: "Make [detail] sharper, more dimensional"

### One Change at a Time
Batch iterations by focusing on exactly ONE variable per round:
1. Composition only
2. Style only
3. Details only

Never change composition AND style AND color in the same iteration — you won't know what worked.

---

## Failure Recovery Guide

| Failure Mode | Symptom | Fix Prompt |
|-------------|---------|-----------|
| **Text artifacts** | Garbled characters appear | "Regenerate with zero text, letters, or characters visible anywhere" |
| **Product deformation** | Distorted proportions | "Ensure the [product] has normal, undistorted shape and proportions" |
| **Style mismatch** | Doesn't match reference | "Shift closer to [style description] feel" |
| **Missing elements** | Required object absent | "Must include [element] at [position]" |
| **Color bleeding** | Colors muddy | "Clean up color palette, more distinct separation between [A] and [B]" |

---

## ⚠️ Hard Rules (STRICT — Violations produce unusable output)

1. **MUST NOT generate any text/characters/words in images**. AI-generated text is always distorted or unreadable. Reserve clean blank areas for text overlay in post-production. This is the #1 failure mode.
2. **MUST write all three layers** (subject-scene, style-texture, technical-constraints) before generating. A single-layer prompt is guaranteed to produce unpredictable results.
3. **MUST NOT change more than one variable per iteration**. If you change composition AND style simultaneously, you cannot attribute the result to either change.
4. **MUST specify exact dimensions/ratio in Layer 3**. Without ratio constraints, models default to square (1:1) which is wrong for most platforms.
5. **MUST include a "no text" or "no watermark" negative prompt** in Layer 3 for every commercial image generation.
6. **MUST NOT skip the style reference (Layer 2)** for Chinese-market images. Using generic "modern" or "minimal" produces inferior results compared to "国潮风", "新中式", "复古港风".

## Key Principles (避坑指南)

1. **Text is forbidden territory**: Generated text is never usable. Always reserve clean space for post-production text overlay.
2. **One change at a time**: Iterate on composition, style, or detail — never all three simultaneously.
3. **Split complex compositions**: Multiple products? Generate the scene first, then composite products in post-processing.
4. **Embrace Chinese aesthetics**: Explicitly use tags like "国潮风", "新中式", "复古港风". These produce dramatically better results than generic "modern" or "minimal" tags in Chinese-market models.
5. **Archive successful prompts**: Save effective prompt combinations for team reuse and knowledge base building.
6. **Layer discipline**: Always write all three layers. Skipping Layer 2 (style) or Layer 3 (constraints) is the #1 cause of unusable outputs.

## Tool Integration

| Platform | Preferred Ratio | Style Keywords | Notes |
|----------|----------------|----------------|-------|
| Taobao/JD main image | 1:1 or 3:4 | Clean product lighting, white/soft gradient bg | Priority on texture and material clarity |
| Campaign poster | 16:9 or 3:4 | Festive, atmospheric, layered depth | Reserve 20-30% for text |
| WeChat cover | 2.35:1 | Knowledge, calm, minimalist | Text zone always top 1/3 |
| Xiaohongshu cover | 3:4 | Eye-catching, high saturation, "网感" | Text top AND bottom, clean center |
| Douyin cover | 3:4 | Bold contrast, energetic | Extreme visual punch needed |
