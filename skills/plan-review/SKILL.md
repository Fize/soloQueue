---
name: plan-review
description: |
  Standardized pre-market preparation, noon review, and evening review workflows plus trading journal management.
  三段式交易操作系统：盘前准备→午间复盘→晚间复盘，配合交易日志工具记录每一天。
  核心原则：脚本取数据，AI 做分析，交易者负责最终决策。
triggers:
  - "盘前准备"
  - "午间复盘"
  - "晚间复盘"
  - "交易计划"
  - "复盘"
  - "写日志"
  - "交易日志"
  - "今日计划"
  - "纪律打分"
  - "plan review"
  - "trading journal"
  - "pre-market"
  - "evening review"
upstream: https://github.com/Fize/mmtickerlab
branch: master
subpath: skills/plan-review
---

# Plan & Review Skill

This is a remote skill catalog entry. Install it to get the full plan-review workflow.

## Upstream

- Repo: https://github.com/Fize/mmtickerlab
- Branch: master
- SubPath: skills/plan-review

## Overview

The plan-review skill provides the **process backbone** for A-share trading. It connects the `market` data skill and `sim-trade` execution skill into a disciplined daily workflow.

### Three Daily Checkpoints

| Checkpoint | Who fetches data | Who analyzes |
|:---|:---|:---|
| **Pre-Market** (盘前准备) | `pre_market.py` | AI synthesizes plan |
| **Noon Review** (午间复盘) | `noon_review.py` | AI checks alignment |
| **Evening Review** (晚间复盘) | `evening_review.py` | AI performs deep 复盘 |

### Journal Script

The `journal.py` script manages the trading journal — the central artifact of this workflow:

| Command | Description |
|:---|:---|
| `create` | Create today's journal (reads plan from `--plan` or stdin) |
| `append --section {noon,evening}` | Append a review section |
| `view [--date YYYYMMDD]` | View a journal |
| `list [--n N]` | List recent entries |

### Prerequisites

This skill depends on:

1. **`market` skill** — provides data faucet scripts (`pre_market.py`, `noon_review.py`, `evening_review.py`)
2. **`sim-trade` skill** — provides portfolio check and trade verification
3. **Python environment** (stdlib only for journal.py)

## Files

When installed from upstream, the skill includes:

```
skills/plan-review/
├── SKILL.md              # This file
├── scripts/
│   └── journal.py         # Journal management CLI
├── templates/
│   ├── pre_market.md      # Pre-market output template
│   ├── noon_review.md     # Noon review output template
│   └── evening_review.md  # Evening review output template
└── requirements.txt       # No external deps (stdlib only)
```
