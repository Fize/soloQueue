# Pitfalls & Verification

> Referenced by `fullstack-dev` SKILL.md. Avoid these anti-patterns and follow the verification protocol.

---

## Pitfalls

- **Skipping think-then-code**: The most common mistake under vague requirements. You MUST write down your understanding and approach BEFORE writing any code. No exceptions.
- **Over-engineering**: Introducing abstraction layers when there's only one implementation. Violates Simplicity. It increases maintenance cost, not reduces it.
- **Drive-by refactoring**: "While I'm here" refactoring during a bug fix or feature. Violates Surgical Changes. It introduces uncontrolled risk.
- **No verification instructions**: Ending a task after writing code. Violates Goal-Driven. The user CANNOT confirm the fix works.
- **Hardcoded env config**: Embedding config values or paths in code. It WILL break in deployment. ALWAYS use env vars.

---

## Verification

After completing any scenario flow, you MUST output ALL of the following:

1. **Change Summary**: which files changed, what changed in each
2. **Verification Commands**: concrete curl / test / browser steps that the user can run
3. **Rollback Plan**: how to revert (git revert command or DB rollback SQL)
4. **Context Summary** (complex tasks only): key decisions, dependencies, follow-up items
