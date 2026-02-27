import re
from typing import Set

# Safe commands that don't need user approval
SAFE_COMMANDS: Set[str] = {
    "ls", "cat", "pwd", "echo", "head", "tail", "wc",
    "grep", "find", "which", "env", "date", "whoami",
    "git status", "git log", "git diff",
    "uv run pytest",
}

# Shell metacharacters that enable command chaining/injection
_SHELL_INJECTION_PATTERN = re.compile(
    r"[;|&`$]"       # semicolon, pipe, ampersand, backtick, dollar
    r"|&&"            # logical AND
    r"|\|\|"          # logical OR
    r"|\$\("          # command substitution $(...)
    r"|>\s*"          # output redirection
    r"|<\s*"          # input redirection
    r"|\n"            # embedded newline
)


def _contains_shell_injection(command: str) -> bool:
    """
    Detect shell metacharacters that could chain dangerous commands.

    Examples that should be caught:
        - "ls; rm -rf /"
        - "cat foo | bash"
        - "echo `whoami`"
        - "ls && rm -rf /"
        - "echo $(cat /etc/passwd)"
        - "ls > /etc/cron.d/evil"

    Args:
        command: Raw command string.

    Returns:
        True if the command contains injection-capable metacharacters.
    """
    return bool(_SHELL_INJECTION_PATTERN.search(command))


def is_safe_command(command: str) -> bool:
    """
    Check if a command is generally considered safe (read-only or informative).

    A command is safe only when:
    1. Its base command (first word or multi-word prefix) is in the allowlist, AND
    2. It does NOT contain shell metacharacters that could chain additional commands.

    Args:
        command: The command string.

    Returns:
        True if the command is safe.
    """
    cmd = command.strip()
    if not cmd:
        return True

    # Reject any command with shell injection metacharacters
    if _contains_shell_injection(cmd):
        return False

    # Check exact match or prefix against allowlist
    parts = cmd.split()
    first_word = parts[0]

    if first_word in SAFE_COMMANDS:
        return True

    # Check full command for multi-word allowlist items
    for safe_cmd in SAFE_COMMANDS:
        if cmd == safe_cmd or cmd.startswith(safe_cmd + " "):
            return True

    return False

