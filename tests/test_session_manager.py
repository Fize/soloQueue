"""
Tests for SessionManager and SessionLogger session management changes.

Covers:
- session_id generation (user_id + date + seq)
- Cross-day expiration
- /new command handling
- RAG archival
- parse_session_id
- ConversationTurn user_id field
- get_sessions_by_user / get_session_turns_text
"""

import json
import tempfile
import shutil
from datetime import datetime
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from soloqueue.core.memory.session_logger import (
    SessionLogger,
    ConversationTurn,
    AIResponse,
)
from soloqueue.core.memory.session_manager import SessionManager, SessionInfo


# ==================== Fixtures ====================


@pytest.fixture
def temp_workspace():
    """Create temporary workspace directory."""
    temp_dir = tempfile.mkdtemp()
    yield temp_dir
    shutil.rmtree(temp_dir, ignore_errors=True)


@pytest.fixture
def session_logger(temp_workspace):
    """Create a SessionLogger with temp workspace."""
    return SessionLogger(temp_workspace)


@pytest.fixture
def session_manager(session_logger):
    """Create a SessionManager with the session logger."""
    return SessionManager(session_logger)


def _save_turn(
    logger: SessionLogger,
    session_id: str,
    turn: int,
    user_msg: str,
    ai_content: str,
    user_id: str = "",
):
    """Helper to save a conversation turn."""
    ct = ConversationTurn(
        session_id=session_id,
        turn=turn,
        timestamp=datetime.now().isoformat(),
        group="test",
        entry_agent="test_agent",
        user_id=user_id,
        user_message=user_msg,
        ai_response=AIResponse(content=ai_content),
    )
    logger.save_turn(ct)


# ==================== SessionManager: parse_session_id ====================


class TestParseSessionId:
    """Tests for SessionManager.parse_session_id."""

    def test_simple_user_id(self):
        user_id, date, seq = SessionManager.parse_session_id("alice_2026-02-27_0")
        assert user_id == "alice"
        assert date == "2026-02-27"
        assert seq == 0

    def test_user_id_with_underscore(self):
        user_id, date, seq = SessionManager.parse_session_id(
            "wecom_zhang_san_2026-02-27_3"
        )
        assert user_id == "wecom_zhang_san"
        assert date == "2026-02-27"
        assert seq == 3

    def test_web_debug_user_id(self):
        user_id, date, seq = SessionManager.parse_session_id(
            "web_debug_2026-02-27_0"
        )
        assert user_id == "web_debug"
        assert date == "2026-02-27"
        assert seq == 0

    def test_invalid_format_no_seq(self):
        with pytest.raises(ValueError):
            SessionManager.parse_session_id("alice_2026-02-27")

    def test_invalid_format_bad_seq(self):
        with pytest.raises(ValueError):
            SessionManager.parse_session_id("alice_2026-02-27_abc")

    def test_invalid_format_no_date(self):
        with pytest.raises(ValueError):
            SessionManager.parse_session_id("alice_0")

    def test_invalid_format_empty_user_id(self):
        with pytest.raises(ValueError):
            SessionManager.parse_session_id("_2026-02-27_0")


# ==================== SessionManager: resolve_session ====================


class TestResolveSession:
    """Tests for SessionManager.resolve_session."""

    def test_new_session_for_new_user(self, session_manager):
        info = session_manager.resolve_session("alice", "2026-02-27")
        assert info.session_id == "alice_2026-02-27_0"
        assert info.user_id == "alice"
        assert info.date == "2026-02-27"
        assert info.seq == 0
        assert info.is_new is True

    def test_reuse_existing_session(self, session_manager, session_logger):
        # Create existing session data
        _save_turn(
            session_logger,
            "alice_2026-02-27_0",
            1,
            "hello",
            "hi",
            user_id="alice",
        )

        info = session_manager.resolve_session("alice", "2026-02-27")
        assert info.session_id == "alice_2026-02-27_0"
        assert info.is_new is False

    def test_cross_day_creates_new_session(self, session_manager, session_logger):
        # Day 1 session exists
        _save_turn(
            session_logger,
            "alice_2026-02-27_0",
            1,
            "hello",
            "hi",
            user_id="alice",
        )

        # Day 2 should create new session
        info = session_manager.resolve_session("alice", "2026-02-28")
        assert info.session_id == "alice_2026-02-28_0"
        assert info.is_new is True

    def test_default_date_is_today(self, session_manager):
        today = datetime.now().strftime("%Y-%m-%d")
        info = session_manager.resolve_session("alice")
        assert info.date == today


# ==================== SessionManager: force_new_session ====================


class TestForceNewSession:
    """Tests for SessionManager.force_new_session (the /new command)."""

    def test_first_new_session_of_day(self, session_manager):
        info = session_manager.force_new_session("alice")
        today = datetime.now().strftime("%Y-%m-%d")
        assert info.session_id == f"alice_{today}_0"
        assert info.seq == 0
        assert info.is_new is True

    def test_increment_seq_on_new(self, session_manager, session_logger):
        today = datetime.now().strftime("%Y-%m-%d")
        # Create existing session
        _save_turn(
            session_logger,
            f"alice_{today}_0",
            1,
            "hello",
            "hi",
            user_id="alice",
        )

        info = session_manager.force_new_session("alice")
        assert info.session_id == f"alice_{today}_1"
        assert info.seq == 1

    def test_multiple_new_sessions(self, session_manager, session_logger):
        today = datetime.now().strftime("%Y-%m-%d")
        # Create two existing sessions
        _save_turn(
            session_logger,
            f"alice_{today}_0",
            1,
            "hello",
            "hi",
            user_id="alice",
        )
        _save_turn(
            session_logger,
            f"alice_{today}_1",
            1,
            "world",
            "!",
            user_id="alice",
        )

        info = session_manager.force_new_session("alice")
        assert info.session_id == f"alice_{today}_2"
        assert info.seq == 2


# ==================== SessionManager: archive_session ====================


class TestArchiveSession:
    """Tests for SessionManager.archive_session."""

    def test_archive_with_turns(self, session_manager, session_logger):
        session_id = "alice_2026-02-27_0"
        _save_turn(session_logger, session_id, 1, "hello", "hi", user_id="alice")
        _save_turn(session_logger, session_id, 2, "bye", "goodbye", user_id="alice")

        mock_memory = MagicMock()
        mock_memory.add_knowledge.return_value = "entry_123"

        result = session_manager.archive_session(session_id, "alice", mock_memory)
        assert result is True

        # Verify add_knowledge was called
        mock_memory.add_knowledge.assert_called_once()
        call_args = mock_memory.add_knowledge.call_args
        content = call_args[1]["content"] if "content" in call_args[1] else call_args[0][0]
        metadata = call_args[1]["metadata"] if "metadata" in call_args[1] else call_args[0][1]

        assert "hello" in content
        assert "hi" in content
        assert "bye" in content
        assert "goodbye" in content
        assert metadata["user_id"] == "alice"
        assert metadata["session_id"] == session_id
        assert metadata["type"] == "session_archive"
        assert metadata["turn_count"] == 2

    def test_archive_empty_session(self, session_manager):
        mock_memory = MagicMock()
        result = session_manager.archive_session(
            "alice_2026-02-27_0", "alice", mock_memory,
        )
        assert result is False
        mock_memory.add_knowledge.assert_not_called()

    def test_archive_handles_error(self, session_manager, session_logger):
        session_id = "alice_2026-02-27_0"
        _save_turn(session_logger, session_id, 1, "hello", "hi", user_id="alice")

        mock_memory = MagicMock()
        mock_memory.add_knowledge.side_effect = RuntimeError("embedding failed")

        result = session_manager.archive_session(session_id, "alice", mock_memory)
        assert result is False


# ==================== SessionLogger: user_id in ConversationTurn ====================


class TestConversationTurnUserId:
    """Tests for user_id field in ConversationTurn."""

    def test_user_id_in_to_dict(self):
        ct = ConversationTurn(
            session_id="alice_2026-02-27_0",
            turn=1,
            timestamp="2026-02-27T10:00:00",
            group="test",
            entry_agent="agent1",
            user_id="alice",
            user_message="hello",
        )
        d = ct.to_dict()
        assert d["user_id"] == "alice"

    def test_empty_user_id_not_in_dict(self):
        ct = ConversationTurn(
            session_id="old_session",
            turn=1,
            timestamp="2026-02-27T10:00:00",
            group="test",
            entry_agent="agent1",
            user_message="hello",
        )
        d = ct.to_dict()
        assert "user_id" not in d

    def test_backward_compatibility_default(self):
        ct = ConversationTurn(
            session_id="test",
            turn=1,
            timestamp="now",
            group="g",
            entry_agent="a",
        )
        assert ct.user_id == ""


# ==================== SessionLogger: get_sessions_by_user ====================


class TestGetSessionsByUser:
    """Tests for SessionLogger.get_sessions_by_user."""

    def test_returns_user_sessions(self, session_logger):
        _save_turn(
            session_logger,
            "alice_2026-02-27_0",
            1,
            "hello",
            "hi",
            user_id="alice",
        )
        _save_turn(
            session_logger,
            "bob_2026-02-27_0",
            1,
            "hey",
            "yo",
            user_id="bob",
        )
        _save_turn(
            session_logger,
            "alice_2026-02-28_0",
            1,
            "morning",
            "gm",
            user_id="alice",
        )

        alice_sessions = session_logger.get_sessions_by_user("alice")
        assert alice_sessions == ["alice_2026-02-27_0", "alice_2026-02-28_0"]

        bob_sessions = session_logger.get_sessions_by_user("bob")
        assert bob_sessions == ["bob_2026-02-27_0"]

    def test_empty_for_unknown_user(self, session_logger):
        assert session_logger.get_sessions_by_user("nobody") == []

    def test_deduplicates_sessions(self, session_logger):
        _save_turn(
            session_logger,
            "alice_2026-02-27_0",
            1,
            "hello",
            "hi",
            user_id="alice",
        )
        _save_turn(
            session_logger,
            "alice_2026-02-27_0",
            2,
            "bye",
            "cya",
            user_id="alice",
        )

        sessions = session_logger.get_sessions_by_user("alice")
        assert sessions == ["alice_2026-02-27_0"]


# ==================== SessionLogger: get_session_turns_text ====================


class TestGetSessionTurnsText:
    """Tests for SessionLogger.get_session_turns_text."""

    def test_formats_turns_correctly(self, session_logger):
        session_id = "alice_2026-02-27_0"
        _save_turn(session_logger, session_id, 1, "hello", "hi", user_id="alice")
        _save_turn(session_logger, session_id, 2, "bye", "goodbye", user_id="alice")

        text = session_logger.get_session_turns_text(session_id)
        assert "User: hello" in text
        assert "AI: hi" in text
        assert "User: bye" in text
        assert "AI: goodbye" in text
        assert "---" in text

    def test_empty_session_returns_empty(self, session_logger):
        text = session_logger.get_session_turns_text("nonexistent_2026-02-27_0")
        assert text == ""

    def test_single_turn(self, session_logger):
        session_id = "alice_2026-02-27_0"
        _save_turn(session_logger, session_id, 1, "hello", "hi", user_id="alice")

        text = session_logger.get_session_turns_text(session_id)
        assert text == "User: hello\nAI: hi"
        assert "---" not in text
