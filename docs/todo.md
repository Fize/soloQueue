# Plan & Task Store (Todo System)

**Location**: `internal/todo/` (service and database store), `internal/sqlitedb/` (shared database connection)

SoloQueue includes a native project management and issue-tracking engine. It allows users and agents to organize work into high-level **Issues** (or plans) containing sub-tasks called **Todo Items** with explicit dependency constraints.

---

## Core Entities & Schema

The Todo System is backed by SQLite, sharing the database connection pool in `entries.db`.

```
                  ┌───────────────┐
                  │     Issue     │
                  └───────┬───────┘
                          │ 1
                          │
                          │ *
                  ┌───────▼───────┐
                  │   TodoItem    │
                  └───────┬───────┘
                          │ *
                          │
                          │ *
                  ┌───────▼───────┐
                  │ Dependency    │
                  └───────────────┘
```

### 1. Issue (originally Plan)
Represents a project or a major feature requirement.
- **`status`**: State machine transition: `backlog` ➔ `todo` ➔ `running` ➔ `done`.
- **`author`**: The user or agent that created the issue.
- **`plan`**: Markdown description of the execution approach.

### 2. Todo Item
An individual granular task associated with an issue.
- **`completed`**: Boolean flag (stored as `0` or `1` in SQLite).
- **`sort_order`**: Integer for user/agent manual reordering.

### 3. Todo Dependency
Defines execution ordering between tasks. A todo item cannot be started or completed until all its dependencies are resolved.

### SQLite Schema
```sql
CREATE TABLE IF NOT EXISTS issue (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    plan TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'backlog' CHECK(status IN ('backlog','todo','running','done')),
    tags TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS todo_items (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    completed INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS todo_dependencies (
    todo_id TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
    depends_on TEXT NOT NULL REFERENCES todo_items(id) ON DELETE CASCADE,
    PRIMARY KEY (todo_id, depends_on)
);
```

---

## Task Completion Constraints

The `todo.Service` (`service.go`) enforces strict rules on state transitions:
1. When a user or agent attempts to toggle a task (`ToggleTodoItem`) from uncompleted to completed:
   - The service fetches the item's dependency list from the `todo_dependencies` table.
   - For each dependency (`depends_on`), the service queries the database.
   - If any dependency item has `completed = 0`, the transaction is aborted and returns an error: `cannot complete: dependency "X" is not yet completed`.
2. Toggling an already completed item back to uncompleted is unrestricted (no dependency check is executed).

---

## Circular Dependency Prevention (Cycle Detection)

When defining dependencies between tasks (`SetDependencies`), SoloQueue runs a validation check to prevent circular references (e.g. Task A depends on Task B, Task B depends on Task A).

### BFS Cycle Detection Algorithm
The cycle check is implemented using a Breadth-First Search (BFS) algorithm in `checkCycle`:

1. **Input**: A target `todoID` and a list of new dependencies `newDeps`.
2. **Reversibility Check**: Ensure that no item in `newDeps` is identical to `todoID` (self-dependency is rejected).
3. **Graph Traversal**:
   - Initialize a FIFO queue with `newDeps` and a `visited` set to prevent infinite loops.
   - While the queue is not empty:
     - Dequeue the `current` task ID.
     - If `current == todoID`, a path exists from the new dependencies back to the target item. A cycle is detected! Abort the transaction and return: `cycle detected: adding these dependencies would create a circular dependency`.
     - Mark `current` as visited.
     - Query the database to find all transitive dependencies of `current`:
       ```sql
       SELECT depends_on FROM todo_dependencies WHERE todo_id = ?
       ```
     - Enqueue any unvisited transitive dependencies.
4. **Completion**: If the queue becomes empty without hitting `todoID`, the dependency update is valid and is committed to the database.

---

## Collaboration & Comments

Issues support comment threads (`issue_comments` table) to allow multiple agents or users to collaborate:
- **Comments Schema**: `id`, `issue_id` (foreign key), `author`, `content` (Markdown), and `created_at`.
- Comments are fetched chronologically during UI page load, providing a detailed design discussion history for each issue.
