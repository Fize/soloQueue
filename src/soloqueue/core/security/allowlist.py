from typing import Set

# Safe commands that don't need user approval
SAFE_COMMANDS: Set[str] = {
    "ls", "cat", "pwd", "echo", "head", "tail", "wc",
    "grep", "find", "which", "env", "date", "whoami",
    "git status", "git log", "git diff",
    "uv run pytest", # Allow running self-tests
}

def is_safe_command(command: str) -> bool:
    """
    Check if a command is generally considered safe (read-only or informative).
    
    Args:
        command: The command string.
        
    Returns:
        True if the command starts with a safe command.
    """
    cmd = command.strip()
    if not cmd:
        return True
        
    # Check exact match or prefix
    parts = cmd.split()
    first_word = parts[0]
    
    # Simple whitelist check on the first word
    if first_word in SAFE_COMMANDS:
        return True
        
    # Check full command for multi-word allowlist items
    for safe_cmd in SAFE_COMMANDS:
        if cmd == safe_cmd or cmd.startswith(safe_cmd + " "):
            return True
            
    return False
