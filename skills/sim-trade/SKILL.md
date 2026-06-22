---
name: sim-trade
description: |
  A-share simulation trading tool. Support resetting the portfolio, checking holdings/P&L, buying stocks, selling stocks, and querying transaction history under strict A-share trading rules (T+1, lot size, commissions, stamp tax, trading hours). A股模拟交易工具，支持账户初始化、持仓及盈亏查询、买入、卖出及交易历史查询。
when_to_use: "Trigger when the user mentions: 模拟交易, 买入, 卖出, 持仓, 模拟盘, simulation trading, paper trading, sim-trade"
upstream: https://github.com/Fize/mmtickerlab
branch: master
subpath: skills/sim-trade
---

# sim-trade

This is a remote skill catalog entry.

## Upstream

- Repo: https://github.com/Fize/mmtickerlab
- Branch: master
- SubPath: skills/sim-trade

## Core Rules

- **T+1 Settlement**: Stocks bought today can only be sold next day+
- **Lot Size**: Buy orders must be multiples of 100 shares
- **Price Limits**: Normal ±10%, ST ±5%, ChiNext ±20%, Star Market ±20%, BJSE ±30%
- **Trading Hours**: Weekdays 9:30-11:30, 13:00-15:00
- **Fees**: Commission 0.025% (min ¥5), Stamp tax 0.1% (sell only), Transfer 0.002%

## Scripts

Full Python scripts available after install:
- `reset.py` — Initialize/reset simulation account
- `portfolio.py` — View positions, cash, P&L
- `buy.py` — Place buy orders
- `sell.py` — Place sell orders
- `history.py` — View transaction logs

## Requirements

Python dependencies: `akshare`, `pandas`, `requests`
