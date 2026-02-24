"""
Deterministic color palette for agent UI differentiation.

Provides consistent color assignment for agents based on their names,
with fallback to a WCAG 2.1 AA compliant palette when no custom color is specified.
"""

import hashlib
from typing import Optional

# 12 distinct colors with good contrast (WCAG 2.1 AA compliant)
# Source: Tailwind CSS default palette (600 series) + adjustments for accessibility
COLOR_PALETTE = [
    "#dc2626",  # red-600
    "#ea580c",  # orange-600
    "#d97706",  # amber-600
    "#ca8a04",  # yellow-600
    "#16a34a",  # green-600
    "#059669",  # emerald-600
    "#0d9488",  # teal-600
    "#0891b2",  # cyan-600
    "#0284c7",  # sky-600
    "#2563eb",  # blue-600
    "#4f46e5",  # indigo-600
    "#7c3aed",  # violet-600
]

PALETTE_SIZE = len(COLOR_PALETTE)


def hash_string(s: str) -> int:
    """
    Deterministic hash of a string to an integer.

    Uses SHA256 for consistent cross-session results.
    """
    # Encode to bytes, hash, convert to integer
    hash_bytes = hashlib.sha256(s.encode("utf-8")).digest()
    # Use first 8 bytes for 64-bit integer
    return int.from_bytes(hash_bytes[:8], byteorder="big")


def get_color_from_palette(agent_name: str) -> str:
    """
    Get a deterministic color from the palette based on agent name.

    Args:
        agent_name: Name of the agent

    Returns:
        Hex color string from COLOR_PALETTE
    """
    hash_val = hash_string(agent_name)
    index = hash_val % PALETTE_SIZE
    return COLOR_PALETTE[index]


def normalize_color(color_str: Optional[str]) -> Optional[str]:
    """
    Basic validation of CSS color strings.

    Currently only checks for empty/None strings.
    More comprehensive validation could be added but is deferred
    to browser CSS parsing (invalid colors will be ignored).

    Args:
        color_str: CSS color value (hex, named, rgb, etc.)

    Returns:
        color_str if non-empty, else None
    """
    if not color_str or not color_str.strip():
        return None
    return color_str.strip()


def get_agent_color(agent_name: str, custom_color: Optional[str] = None) -> str:
    """
    Determine the color to use for an agent's UI elements.

    Priority:
    1. Custom color (if provided and valid)
    2. Deterministic palette color based on agent name

    Args:
        agent_name: Name of the agent
        custom_color: Optional custom color from agent configuration

    Returns:
        CSS color string (hex, named, or rgb)
    """
    # Try to use custom color first
    normalized_custom = normalize_color(custom_color)
    if normalized_custom:
        return normalized_custom

    # Fallback to deterministic palette
    return get_color_from_palette(agent_name)