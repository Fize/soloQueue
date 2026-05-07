# Sample Soul Profiles

This directory contains ready-to-use **soul profiles** — system prompts that define the
identity, voice, and behavior of the L1 (top-level) assistant in SoloQueue.

Each file in this directory is a self-contained character profile. You can drop any of
them into your assistant's role directory as `soul.md` and the assistant will adopt that
persona on the next launch.

## How a Soul Is Loaded

On startup, SoloQueue looks for the active role's soul at:

```
<BaseDir>/roles/<RoleID>/soul.md
```

- `<BaseDir>` is the prompts base directory (typically `<workDir>/prompts`).
- `<RoleID>` is the active role identifier (e.g. `main_assistant`).

If `soul.md` is missing, SoloQueue launches the onboarding questionnaire and writes a
generic profile based on your answers. To use one of the profiles here instead, just copy
it into place **before** starting the app:

```bash
cp docs/roles/hanli.md <BaseDir>/roles/<RoleID>/soul.md
```

You can also open `soul.md` in any editor at any time and paste in the content of one of
these files manually.

## Included Profiles

All sample profiles are written in English and share a common structure. The character's
native name may appear in its original script (for example `韩立`, `本座`); this is intentional
and part of the voice the profile is trying to reproduce.

| File | Character | One-line description |
| --- | --- | --- |
| `hanli.md` | 韩立 (Han Li) | A calm, taciturn rogue cultivator — cautious, measured, and always prepared with an escape route. |
| `jiyin.md` | 极阴老祖 (Ancestor Ji Yin) | A domineering, millennia-old devil-path patriarch — imperious, predatory, and ruthlessly pragmatic. |
| `nangongwan.md` | 南宫婉 (Nangong Wan) | A clear, cold, aloof cultivator — sparse of words, firm of Dao-heart, decisive in action. |
| `xuangu.md` | 玄骨上人 (Venerable Xuan Gu) | A sinister, ancient elder — every word a trap, every alliance a prepared coffin. |
| `yuanyao.md` | 元瑶 (Yuan Yao) | Gentle and soft-spoken with inner resilience — keeps a silent debt-of-gratitude ledger. |
| `ziling.md` | 紫灵 (Zi Ling / Wang Ning) | A measured, calculating strategist — every move weighed for cost, timing, and retreat. |

## Recommended Structure For Your Own Soul

If you want to write your own soul instead of copying one of the samples, the structure
below works well for role-play-style prompts. None of these sections are mandatory; they
are recommendations only.

1. `## Character Identity` — who the assistant is in one compact paragraph.
2. `## Expression Style` — tone, signature phrases, and when they switch registers.
3. `## Mental Model` — core beliefs, worldview, hidden calculus.
4. `## Decision Heuristics` — default instincts, key questions, risk strategy.
5. `## Behavioral Constraints` — hard "never do this" rules.
6. `## Honesty Boundaries` — self-acknowledged limitations and blind spots.
7. `## Work Principles` — operational rules (delegation, scope, plan-before-action, etc.).

## First-Line Convention

SoloQueue's sidebar parses the assistant's display name from the very first line of the
soul. To make sure the name is detected correctly, start the file with:

```
You are <Name>, a personal assistant and the single point of interaction for the user.
```

Anything in `<Name>` up to the first `, a` / `, an` delimiter will be used as the display
name. For example `You are Han Li, a personal assistant...` yields the display name
`Han Li`.

## Notes

- These files are **samples**, not runtime data. Editing them in place will not affect an
  already-configured assistant — you still need to copy or paste the content into
  `soul.md` for the relevant role.
- Feel free to fork, mix, and remix. A soul is just a Markdown document.
