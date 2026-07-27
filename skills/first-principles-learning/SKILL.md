---
name: first-principles-learning
description: 'Systematically learn, study, or analyze any subject from first principles and produce a structured learning document the user can read and review. Use when the user wants to understand, study, onboard to, or reverse-engineer something — not just get a surface-level explanation. Triggers include: "learn/study/understand X", "explain X from first principles", "analyze X systematically", "help me understand this codebase/library/tech", "onboard me to X", "reverse-engineer how X works", "break down X to the fundamentals". Applies to ANY domain: source code / open-source libraries, technical concepts & theories, business models & systems, skills & crafts, organizational or product designs.'
---

# First-Principles Learning

Turn any subject into a clear, verifiable mental model and a reusable learning
document. The method refuses "it just is" authority and always asks "why can't it
be otherwise?"

---

## Why these five stages

First principles = reduce conclusions back to irreducible facts, then re-derive
from those facts. The five stages map to:

1. **Goal (reflect)** — Without a goal, research drifts. The goal is "what the
   learner can *do or decide* after studying this". Inferred by the agent from
   context; never asked of the user.
2. **Problem origin** — Establish the problem space first; otherwise code and
   solutions are anchorless symbols.
3. **Atomic primitives** — Reduce to the axiom layer; this is the foundation for
   all subsequent reasoning.
4. **Reconstructed model** — Reassemble primitives into the minimal runnable
   model; inability to build it = incomplete understanding.
5. **Derivation under constraints + prediction validation** — Distinguish essential
   constraints from incidental decisions; use prediction instead of memorization.

Core discipline: refuse "it just is" authority; always ask "why can't it be
otherwise?"

---

## Core workflow

Run these stages in order. Capture findings into the output document as you go
(see `references/document-template.md` for the section structure — load it before
writing the final doc).

### Stage 0 — Goal inference (reflect, don't ask)

Do NOT ask the user. Instead, ask yourself:
- What is the most common learner intent for this topic? (hands-on use /
  technology comparison / secondary development / deep dive / teaching others)
- Who is the most likely reader? (yourself, a colleague, a new onboarder)
- What is the single "ultimate question" the learner should be able to answer?

> This is **agent-internal calibration only** — it determines document depth and
> entry angle. Do **not** surface "learning goal / target reader" meta lines in
> the final document. The document starts at the title and goes straight into
> the subject.

### Stage 1 — Problem grounding

Identify the **fundamental problem/need** the subject addresses. Scenario prompts:

- **Code / library**: What engineering pain does it solve? How was it done before?
  Where exactly was the pain?
- **Concept / theory**: What phenomenon does it explain? Why was the old
  explanation insufficient?
- **Business / system**: What is the customer's Jobs-To-Be-Done (JTBD)? What
  value did the old solution miss?
- **Skill / craft**: What outcome must be produced? Where do beginners get stuck?

Establish the "problem space" before the "solution space".

### Stage 2 — Atomic primitives

Find the irreducible building blocks / core abstraction. Where to look:

- **Code / library**: Run the minimal use case + step through with a debugger;
  read `git log`/`blame` to find the author's first few commits (often closer to
  the essence than the current layered code); locate the core data structure /
  interface / algorithm.
- **Concept / theory**: Surface the axioms, definitions, and unprovable premises.
- **Business / system**: Draw the value-exchange diagram; label key resources and
  constraints.
- **Skill / craft**: Break down to sub-actions that cannot be further divided.

List each primitive with a one-line essence.

### Stage 3 — Reconstruction (minimal mental model, scenario-conditional)

Recompose primitives into a **minimal mental model**; show how data / value /
logic flows. The "build a toy version" sub-step is **conditional**:

- **Do reproduce a toy version ONLY when** the subject is a *small, concrete code
  functional point* (a primitive / algorithm / single API's semantics) that you
  can reproduce cheaply. Build only the core path. Then read the real source and
  ask "what does it have that mine doesn't?" (often engineering, performance, or
  security depth). Can't build it = don't truly understand.
- **Skip reproduction when**: (1) the subject is a large program/system — model
  it in your head or on a diagram; (2) it is mature community/common-sense
  content (e.g. "HTTP is request-response") — no need to reinvent; (3) **ANY
  non-code subject** (business / concept / skill) — replace code with "explain
  the flow + one everyday analogy":
  - Concept/theory: Derive a known conclusion from the primitives (able to derive
    = understands).
  - Business/system: Construct the minimal viable model and predict its response
    to a given input; use an everyday analogy to explain the value exchange.
  - Skill/craft: Describe the action sequence required to reach minimum
    proficiency — do not write code.
- Rule of thumb: only build a toy when *reproduction cost ≪ confidence gained*.

### Stage 4 — Derivation under constraints

For each major design choice ask: **why not the alternative?** Separate:
- *Essential constraints* (physics, hardware, math, law, environment) — must hold;
  record them.
- *Incidental decisions* (history, convention, taste) — can be skimmed quickly.

The gap between the two is where real engineering/design insight lives. Present in
a table: Decision | Alternative | Constraint type | Rationale.

### Stage 5 — Validation by prediction

Produce 2–3 predictions under specific conditions, then cross-check against
source code / experiment / reference material. Record:
Prediction | Actual | What the gap means.
Consistent correct predictions = genuine understanding; wrong predictions =
cognitive blind spots → return to Stage 2–4.

### Stage 6 — Emit the document

Load `references/document-template.md` and write the learning document following
its section structure. Use `scripts/scaffold_doc.py` to create the file skeleton,
or write it directly. Keep it self-contained and readable without further
conversation.

---

## Operating rules

- **Never fabricate.** If you don't know, say so and mark the gap as an open
  question in the document. If you must clarify a genuine blocker, ask at most
  3 focused questions and never repeat the same one.
- **Match the user's language.** Write the output document in the same language
  the user used in their request, or the language they explicitly asked for.
  All internal skill files (SKILL.md, references/, scripts/) are English only;
  only the final learning document adapts to the user's language.
- **Lead with structure, not prose.** Tables, lists, and diagrams over paragraphs.
- **Show the derivation, not just the conclusion.** The "why not otherwise" is the
  point.
- **Output document = learning content only.** Do NOT include meta filler: no
  "inferred goal / reader" lines, no skill-internal judgment, no narration of how
  the skill itself decided things. Those are the agent's private reasoning, not for
  the reader. Start the doc at the title and go straight into the subject.
  - **FORBIDDEN patterns — never appear in the output doc:** any mention of
    methodology stage names, skill-internal labels (e.g. "Stage N", "SKILL",
    "skipping reproduction because large codebase", "this is a non-code topic",
    "can't build it = don't understand", "inferred goal"). Write the content
    directly; never say *that* you skipped or adapted a step. WRONG: "(large
    codebase — skipping toy version per methodology)". RIGHT: just write the
    analogy/flow with no meta comment.
- **Write for a non-expert.** Keep technical primitives as named terms, but explain
  each with plain language + one everyday analogy so a zero-background reader
  builds intuition. Explanation order: first "what it's like", then "what it is",
  terms last. Fewer jargon dumps, more "why would you think this way".
- Scope the depth to the inferred Stage-0 goal; don't over-build for a "just
  explain" request.

---

## Resources

- `references/document-template.md` — the exact section structure for the output
  learning document. **Load before writing the final doc.**
- `scripts/scaffold_doc.py` — writes an empty template skeleton to a path so the
  document starts from a consistent structure.
