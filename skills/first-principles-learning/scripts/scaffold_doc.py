#!/usr/bin/env python3
"""
Scaffold a first-principles learning document from the template.

Usage:
    scaffold_doc.py --topic "Redis" --output redis-learning.md
    scaffold_doc.py --topic "Redis" --output redis.md --scope "Redis official docs"
"""
import argparse
from pathlib import Path

TEMPLATE = '''# First-Principles Study: {topic}

> Source: {scope}

## 1. The Root Problem It Solves
[Problem space: the underlying need the topic addresses; how people coped before
it existed and where that was painful.]

## 2. Atomic Primitives / Core Abstractions
| Primitive | One-line essence | Why it cannot be further divided |
|-----------|-----------------|----------------------------------|
| ...       | ...             | ...                              |

## 3. Minimal Mental Model
[How do the primitives combine? How does data / value / logic flow?
Small code functional point → attach toy version; everything else → analogy + flow.]

## 4. Design Trade-offs and Constraints
| Design decision | Alternative | Constraint type (Essential / Incidental) | Rationale |
|----------------|-------------|------------------------------------------|-----------|
| ...            | ...         | ...                                      | ...       |

## 5. Validation: Predictions vs. Reality
| Prediction | Actual | What the gap means |
|------------|--------|--------------------|
| ...        | ...    | ...                |

## 6. Learning Path / Action Checklist
- [ ] ...
- Open question: ...

## 7. Quick-Reference Card
[1-sentence positioning + 3–5 core primitives + 1 most counter-intuitive design point.]
'''


def main():
    ap = argparse.ArgumentParser(description="Scaffold a first-principles learning doc.")
    ap.add_argument("--topic", required=True, help="Subject being studied")
    ap.add_argument("--output", required=True, help="Target markdown path")
    ap.add_argument("--scope", default="(to be filled in)", help="Source of the analysis, one line")
    args = ap.parse_args()

    content = TEMPLATE.format(
        topic=args.topic,
        scope=args.scope,
    )
    Path(args.output).write_text(content, encoding="utf-8")
    print(f"✅ Learning document skeleton created: {args.output}")


if __name__ == "__main__":
    main()
