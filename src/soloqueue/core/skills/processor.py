import re
import subprocess
import shlex
import os
from typing import Optional
from soloqueue.core.logger import logger

class SkillPreprocessor:
    """
    Processes Skill Content (Prompt Template) before execution.
    Handles:
    1. Variable Substitution ($ARGUMENTS)
    2. Dynamic Command Injection (!command)
    """
    
    def process(self, content: str, arguments: str, skill_dir: str) -> str:
        """
        Hydrate the prompt template.
        :param content: Raw Markdown content from SKILL.md
        :param arguments: User arguments string
        :param skill_dir: Directory to execute commands in
        :return: Fully populated system prompt
        """
        if not content:
            return ""
            
        # 1. Substitute Arguments
        # Simple string replace for now. Could be more robust with regex.
        # Check if $ARGUMENTS is present to avoid confusion if user input contains it.
        processed_content = content.replace("$ARGUMENTS", arguments)
        
        # 2. Execute Command Injections
        # Look for lines starting with '!', but not inside code blocks? 
        # Claude Code spec is simple: lines starting with ! run.
        # We iterate line by line.
        
        lines = processed_content.split('\n')
        final_lines = []
        
        for line in lines:
            stripped = line.strip()
            if stripped.startswith("!"):
                # Extract command
                cmd_str = stripped[1:].strip()
                if not cmd_str:
                    final_lines.append(line) # ignore empty !
                    continue
                    
                logger.info(f"Skill Injection: Executing '{cmd_str}' in {skill_dir}")
                
                try:
                    # Execute command
                    # We use shell=True to support pipes, but valid security concern.
                    # Since Skills are trusted configuration (like code), this is acceptable for MVP.
                    output = subprocess.check_output(
                        cmd_str, 
                        shell=True, 
                        cwd=skill_dir, 
                        stderr=subprocess.STDOUT,
                        text=True,
                        timeout=30 # Prevent hangs
                    )
                    final_lines.append(output.strip())
                except subprocess.CalledProcessError as e:
                    logger.error(f"Injection Failed: {cmd_str} | Error: {e.output}")
                    final_lines.append(f"[Error executing '{cmd_str}': {e.output.strip()}]")
                except Exception as e:
                    logger.error(f"Injection Error: {e}")
                    final_lines.append(f"[System Error executing instruction: {e}]")
            else:
                final_lines.append(line)
        
        return "\n".join(final_lines)
