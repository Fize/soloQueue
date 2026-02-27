"""
SessionManager - Session 生命周期管理器

功能：
1. 基于 user_id + 日期生成确定性 session_id
2. 跨天自动开启新 session
3. 支持 /new 命令手动开启新 session
4. 旧 session 归档到 SemanticStore (RAG)

session_id 格式: {user_id}_{YYYY-MM-DD}_{seq}
  - user_id: 用户标识（由调用方保证唯一性）
  - YYYY-MM-DD: 日期
  - seq: 同日序号（/new 递增）
"""

from dataclasses import dataclass
from datetime import datetime
from typing import Optional

from loguru import logger


@dataclass
class SessionInfo:
    """Session 元数据"""

    session_id: str
    user_id: str
    date: str       # YYYY-MM-DD
    seq: int        # 同日序号（/new 递增）
    is_new: bool    # 是否为本次新创建


class SessionManager:
    """
    Session 生命周期管理器。

    负责 session_id 生成、过期检测、/new 处理、旧 session 归档。
    不依赖 Web 层，可供任意渠道（企微、飞书、Web 等）使用。
    """

    def __init__(self, session_logger: "SessionLogger"):
        """
        初始化 SessionManager。

        Args:
            session_logger: SessionLogger 实例，用于查询历史 session
        """
        self.session_logger = session_logger

    def resolve_session(
        self,
        user_id: str,
        current_date: Optional[str] = None,
    ) -> SessionInfo:
        """
        根据 user_id 和日期解析当前有效 session。

        规则：
        - 查找该用户当天最新的 session_id
        - 如果存在则复用，否则创建新 session（seq=0）
        - 跨天自动创建新 session

        Args:
            user_id: 用户标识
            current_date: 当前日期（YYYY-MM-DD），默认取今天

        Returns:
            SessionInfo 包含 session_id 和元数据
        """
        if current_date is None:
            current_date = datetime.now().strftime("%Y-%m-%d")

        # 查找该用户当天所有 session
        existing_sessions = self._find_user_sessions_for_date(user_id, current_date)

        if existing_sessions:
            # 复用最新的 session
            latest = existing_sessions[-1]
            return SessionInfo(
                session_id=latest,
                user_id=user_id,
                date=current_date,
                seq=self._extract_seq(latest),
                is_new=False,
            )

        # 当天无 session，创建新的（seq=0）
        session_id = self._build_session_id(user_id, current_date, 0)
        return SessionInfo(
            session_id=session_id,
            user_id=user_id,
            date=current_date,
            seq=0,
            is_new=True,
        )

    def force_new_session(self, user_id: str) -> SessionInfo:
        """
        强制创建新 session（/new 命令）。

        seq 在当天已有 session 基础上递增。

        Args:
            user_id: 用户标识

        Returns:
            新创建的 SessionInfo
        """
        current_date = datetime.now().strftime("%Y-%m-%d")
        existing_sessions = self._find_user_sessions_for_date(user_id, current_date)

        if existing_sessions:
            latest_seq = self._extract_seq(existing_sessions[-1])
            new_seq = latest_seq + 1
        else:
            new_seq = 0

        session_id = self._build_session_id(user_id, current_date, new_seq)
        logger.info(f"Force new session: user_id={user_id}, session_id={session_id}")

        return SessionInfo(
            session_id=session_id,
            user_id=user_id,
            date=current_date,
            seq=new_seq,
            is_new=True,
        )

    def get_previous_session_id(self, user_id: str) -> Optional[str]:
        """
        获取用户上一个 session_id（用于归档）。

        查找该用户当天倒数第二个 session（如果有），
        或者前一天最新的 session。

        Args:
            user_id: 用户标识

        Returns:
            上一个 session_id，如果不存在返回 None
        """
        current_date = datetime.now().strftime("%Y-%m-%d")
        today_sessions = self._find_user_sessions_for_date(user_id, current_date)

        if len(today_sessions) >= 2:
            # 当天有多个 session，返回倒数第二个
            return today_sessions[-2]

        # 查找所有 session，找当天之前的最新 session
        all_sessions = self.session_logger.get_sessions_by_user(user_id)
        previous = [s for s in all_sessions if not s.startswith(f"{user_id}_{current_date}_")]

        if previous:
            return previous[-1]

        return None

    def archive_session(
        self,
        session_id: str,
        user_id: str,
        memory_manager: "MemoryManager",
    ) -> bool:
        """
        将旧 session 对话归档到 SemanticStore。

        将 session 的所有对话轮次合并为文本，存入向量库。
        不使用 LLM 摘要，由 SemanticStore 定期 compaction 负责压缩。

        Args:
            session_id: 要归档的 session_id
            user_id: 用户标识
            memory_manager: MemoryManager 实例

        Returns:
            是否归档成功
        """
        try:
            turns_text = self.session_logger.get_session_turns_text(session_id)
            if not turns_text:
                logger.debug(f"No turns to archive for session: {session_id}")
                return False

            # 解析 session_id 获取日期
            __, date, seq = self.parse_session_id(session_id)

            # 获取 turn 数量
            turns = self.session_logger.get_turns(session_id)
            turn_count = len(turns)

            metadata = {
                "type": "session_archive",
                "user_id": user_id,
                "session_id": session_id,
                "date": date,
                "seq": seq,
                "turn_count": turn_count,
            }

            entry_id = memory_manager.add_knowledge(
                content=turns_text,
                metadata=metadata,
            )

            if entry_id:
                logger.info(
                    f"Archived session {session_id}: "
                    f"{turn_count} turns, {len(turns_text)} chars"
                )
                return True

            return False

        except Exception as e:
            logger.error(f"Failed to archive session {session_id}: {e}")
            return False

    @staticmethod
    def parse_session_id(session_id: str) -> tuple[str, str, int]:
        """
        解析 session_id 为 (user_id, date, seq)。

        session_id 格式: {user_id}_{YYYY-MM-DD}_{seq}
        注意 user_id 本身可能包含下划线，日期格式固定为 YYYY-MM-DD（10 字符）。

        Args:
            session_id: session_id 字符串

        Returns:
            (user_id, date, seq) 元组

        Raises:
            ValueError: session_id 格式不合法
        """
        # 从右边分割出 seq
        last_underscore = session_id.rfind("_")
        if last_underscore == -1:
            raise ValueError(f"Invalid session_id format: {session_id}")

        seq_str = session_id[last_underscore + 1:]
        rest = session_id[:last_underscore]

        try:
            seq = int(seq_str)
        except ValueError as e:
            raise ValueError(f"Invalid seq in session_id: {session_id}") from e

        # rest 格式: {user_id}_{YYYY-MM-DD}
        # 日期固定 10 字符，前面有一个 _
        if len(rest) < 11 or rest[-11] != "_":
            raise ValueError(f"Invalid date in session_id: {session_id}")

        date = rest[-10:]
        user_id = rest[:-11]

        if not user_id:
            raise ValueError(f"Empty user_id in session_id: {session_id}")

        return (user_id, date, seq)

    @staticmethod
    def _build_session_id(user_id: str, date: str, seq: int) -> str:
        """构建 session_id 字符串。"""
        return f"{user_id}_{date}_{seq}"

    def _find_user_sessions_for_date(
        self,
        user_id: str,
        date: str,
    ) -> list[str]:
        """
        查找指定用户在指定日期的所有 session_id，按 seq 排序。

        通过扫描 JSONL 文件推导，不引入额外数据库。
        """
        all_sessions = self.session_logger.get_sessions_by_user(user_id)
        prefix = f"{user_id}_{date}_"
        matched = [s for s in all_sessions if s.startswith(prefix)]

        # 按 seq 排序
        def sort_key(sid: str) -> int:
            try:
                return int(sid.rsplit("_", maxsplit=1)[1])
            except (ValueError, IndexError):
                return 0

        matched.sort(key=sort_key)
        return matched

    @staticmethod
    def _extract_seq(session_id: str) -> int:
        """从 session_id 提取 seq 值。"""
        try:
            return int(session_id.rsplit("_", maxsplit=1)[1])
        except (ValueError, IndexError):
            return 0
