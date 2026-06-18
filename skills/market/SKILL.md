---
name: market
description: |
  Query A-share market overview, rankings, limit-up pools, concept/industry fund flows, individual stock profile (realtime, kline with multi-period, cyq cost distribution, institution comments, individual fund flow, financials), individual stock news, and save market reviews. A股市场行情查询与深度分析工具，支持大盘、个股走势、涨停板及资金流向。
triggers:
  - "行情"
  - "大盘"
  - "涨停"
  - "资金流向"
  - "个股分析"
  - "A股"
  - "market overview"
  - "stock profile"
upstream: https://github.com/Fize/mmtickerlab
branch: master
subpath: skills/market
---

# market

This is a remote skill catalog entry.

## Upstream

- Repo: https://github.com/Fize/mmtickerlab
- Branch: master
- SubPath: skills/market

## Scripts

Full Python scripts available after install:
- `overview.py` — Market overview (indices, up/down stats)
- `ranking.py` — Top gainer/loser stocks
- `limit_up.py` — Limit-up pool details
- `fund_flow.py` — Concept/industry capital flows
- `stock_profile.py` — Deep-dive individual stock analysis
- `news.py` — Stock-specific news
- `save_review.py` — Save market reviews

## Data Sources

- 巨潮资讯网 (Cninfo), 同花顺 (10jqka), 东方财富 (Eastmoney), 财联社 (CLS), 天天基金网

## Requirements

Python dependencies: `akshare`, `pandas`, `requests`, `curl_cffi`
