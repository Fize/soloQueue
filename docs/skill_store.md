# Skill Store & Management System

**Location**: `internal/skill/management.go` (management operations), `internal/server/tool_skill_handlers.go` (REST API handlers)

SoloQueue features a robust **Skill Store and Management System** that allows users and agents to browse a centralized skill catalog, install skills locally or from remote Git repositories, toggle skill activation via shadowing, browse files inside a skill, and edit definitions dynamically.

---

## 1. Directory Structure & Layered Scopes

The system manages skills by scanning and loading files across multiple directories:

```
                            Skill Catalog Directories
             ┌─────────────────────────┬─────────────────────────┐
             ▼                         ▼                         ▼
      [ Local Workspace ]      [ User Home Dir ]        [ Embedded FS ]
       (./skills/ or            (~/.soloqueue/store/     (distFS() virtual
        ../skills/)               skills/)                 "skills/")
             │                         │                         │
             └─────────────────────────┼─────────────────────────┘
                                       │ Install
                                       ▼
                            ┌──────────────────────┐
                            │    User Directory    │
                            │ (~/.soloqueue/skills)│
                            └──────────────────────┘
```

- **Catalog Directories (Read-Only)**: Used to list available skills. The system searches:
  1. `./skills/` (project-local catalog)
  2. `../skills/` (parent directory catalog)
  3. `~/.soloqueue/store/skills/` (global fallback catalog)
  4. Embedded Virtual Filesystem (`distFS()`) under `"skills/"` (bundled default catalog)
- **User Installation Directory (Read-Write)**: The target path where active skills are installed and loaded (`~/.soloqueue/skills/`).

---

## 2. Listing Catalog Skills (`GET /api/skills/store`)

When listing the store catalog, the system:
1. Queries the prioritized catalog paths using `getStoreSkills()` to aggregate available skills.
2. Checks if each skill folder already exists in the user's write-write folder (`userSkillsDir`).
3. Reuses the `Enabled` boolean field in the JSON response (`SkillInfoResponse`) to denote the skill's **installed status** (e.g. `true` if already installed locally, `false` otherwise).

---

## 3. Installation Workflows (`POST /api/skills/install`)

The installer supports three source strategies depending on request parameters:

### Strategy A: Catalog Install (`source: "store"`)
Copies a skill from the catalog to the user skills directory.
- **Git Redirection**: If the parsed catalog entry contains an `upstream` Git URL (e.g., pointing to the `anthropics/skills` repository), the installer **automatically redirects** the request to the Git cloning pipeline.
- **Direct Copy**: If no upstream is defined, it reads the files from disk (or virtual `distFS()`) and writes them directly to the user directory.

### Strategy B: Local Symlinking (`source: "local"`)
Designed for skill developers to test changes in real-time.
- Spawns `os.Symlink` linking the external directory to the user skills directory.
- If symlinking fails (e.g. due to OS permissions), falls back to directory copying.
- **Validation**: Rejects directories that do not contain a `SKILL.md` file.

### Strategy C: Remote Git Cloning (`source: "github"`)
Clones a public Git repository directly into the user skills directory. Details below.

---

## 4. Git Cloning Pipeline (`InstallGithubSkill`)

To install remote skills from source control, the system implements a Git command pipeline:

```
                          Request URL & Subpath
                                    │
                         Rejects Branch/File URLs
                           (no /tree/ or /blob/)
                                    │
                        ┌───────────┴───────────┐
                        ▼                       ▼
                  [ Subpath = "" ]       [ Subpath != "" ]
                        │                       │
                  Shallow Clone           Shallow Clone
                 to Target Folder         to Temp Workspace
                        │                       │
                        │               Copy Subfolder Only
                        │               to Target Folder
                        │                       │
                        └───────────┬───────────┘
                                    │
                            Verify SKILL.md
                                    │
                                 Complete
```

### 1. URL Validation
- The Git cloner enforces strict URL constraints. It rejects URLs containing `/tree/` or `/blob/` patterns (which represent specific nested branch/file web views) and requires a clean repository root address.

### 2. Shallow Cloning with Sub-Directory Support
- **Full Repository**: If no `subPath` is specified, it runs a shallow clone:
  ```bash
  git clone --depth 1 -b <branch> <repoUrl> <targetDir>
  ```
  If the specific branch clone fails, it runs a fallback clone command without the branch flag to fetch the default branch.
- **Subpath Extraction**: If a `subPath` is specified (e.g., fetching a single skill from a mono-repo like `skills/docx`):
  - Spawns a temporary workspace (`os.MkdirTemp`).
  - Clones the full repository into the temp directory.
  - Copies *only* the contents of the target subpath to the user skills directory.
  - Recursively deletes the temporary workspace.

### 3. Integrity Check
Before concluding, the installer validates that the target folder contains a valid `SKILL.md` file. If missing, it deletes the copied folder and returns a validation error to prevent corrupt installations.

---

## 5. Shadowing & Overrides

### Toggling Skills (`POST /api/skills/{id}/toggle`)
Instead of deleting files, the system manages skill activation states using a `.disabled` indicator:
- Active registry loaders check for the presence of an empty file named `.disabled` inside the skill directory. If present, the skill is ignored during registration.
- **Toggling On/Off**: The API checks for `.disabled`. If it exists, the file is removed (enabling the skill); if it doesn't, an empty `.disabled` file is written.

### The Shadow Override Pattern
Because built-in catalog skills (like those in `distFS()`) are read-only, users cannot write a `.disabled` file into them.
- To disable a built-in skill, the toggle handler **automatically copies** the skill from the catalog into the user's directory (`userSkillsDir`) to act as a **local override/shadow**.
- The system then writes `.disabled` inside the shadow directory.
- During registry rebuild, the shadow directory overrides the catalog, effectively disabling the skill.

---

## 6. Import & Update Form APIs

Users can author new skills or modify existing ones via the Web UI:
- **`POST /api/skills` (`ImportUserSkill`)**:
  - Validates and **slugifies** the name (converts letters to lowercase, replaces spaces/special characters with a single dash `-`, trims edges, and truncates to 64 characters).
  - Creates the slug directory and writes a generated `SKILL.md` with structured YAML frontmatter.
- **`PUT /api/skills/{id}` (`UpdateUserSkill`)**:
  - Updates the text body or frontmatter fields.
  - If updating a built-in skill, it automatically creates a shadow override directory under `userSkillsDir` and writes the changes, preserving the original catalog files.

---

## 7. File Browser (`GET /api/skills/{id}/files`)

To enable users to inspect custom shell scripts, templates, or instructions bundled inside a skill directory, the system provides a file explorer:
- **Recursion**: Crawls the directory using `ListSkillFiles` or `ListSkillFilesFromFS`.
- **Safety Constraints**:
  - Excludes hidden dot-files (like `.git`).
  - Limits traversal to a maximum depth of **6 layers** and a maximum of **500 entries** to prevent memory exhaustion from large cloned repositories.
  - Restricts access strictly within the target skill directory to prevent directory traversal attacks.
