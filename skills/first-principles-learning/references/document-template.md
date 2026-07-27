# Learning Document Template (Output Skeleton)

Use this structure when writing the final learning document. **Write only the
learning content itself**: no "learning goal / target reader" meta lines, no
skill-process self-narration ("skipping reproduction per methodology", etc.) —
those are not content for the reader. Delete all bracketed prompt hints before
publishing. The scaffold script (`scripts/scaffold_doc.py`) generates an empty
skeleton with the same structure.

> Note: the `> Source` line inside the code block below may be kept in the output
> as a lightweight provenance marker, but **never** include "learning goal /
> reader" lines. All other bracketed prompts are agent instructions — remove them
> in the final document.

> **Language note**: write the output document in the user's language (match the
> language of their request or their explicit preference). The template headings
> below are English references only; translate them in the actual output document
> if the user's language is not English.

```markdown
# First-Principles Study: {Topic}

> Source: {source code / official docs / experiment — one line}

## 1. The Root Problem It Solves
[Problem space: the underlying need the topic addresses; how people coped before
it existed and where that was painful. Problem first, solution second.]

## 2. Atomic Primitives / Core Abstractions
[List each irreducible building block with a one-line essence. A table is better:
Primitive | Essence | Why it is atomic]
| Primitive | One-line essence | Why it cannot be further divided |
|-----------|-----------------|----------------------------------|
| ...       | ...             | ...                              |

## 3. Minimal Mental Model
[How do the primitives combine? How does data / value / logic flow?
- If the subject is a **small, concrete code functional point**: include a minimal
  runnable / toy example (build first, then compare to the real source).
- If the subject is a **large-scale program / mature common knowledge / non-code
  topic**: use "one everyday analogy + flow description" instead of code — do not
  force a toy version.
- **For non-expert readers**: explain every technical primitive with a plain-
  language analogy so a zero-background reader can build intuition.]

## 4. Design Trade-offs and Constraints
[For each key design choice: Decision | Alternative | Essential or Incidental |
Rationale]
| Design decision | Alternative | Constraint type | Rationale |
|----------------|-------------|-----------------|-----------|
| ...            | ...         | Essential / Incidental | ...  |

## 5. Validation: Predictions vs. Reality
[2–3 concrete predictions + cross-check results against source / experiment /
reference. Note the gap and what it means.]
| Prediction | Actual | What the gap means |
|------------|--------|--------------------|
| ...        | ...    | ...                |

## 6. Learning Path / Action Checklist
[What to practise or read next; list any open questions that remain.]
- [ ] ...
- Open question: ...

## 7. Quick-Reference Card
[One-page review: 1-sentence positioning + 3–5 core primitives + 1 most
counter-intuitive design point.]
```

> The quick-reference card must stand alone without the rest of the document —
> a reader who only skims this section should still be able to recall the main
> thread.
