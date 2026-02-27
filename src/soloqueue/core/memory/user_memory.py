"""用户画像存储模块。

管理用户画像 Markdown 文件的读取，所有 Agent 共享同一个 USER.md 文件。
"""

from pathlib import Path

from loguru import logger


USER_PROFILE_TEMPLATE = """# User Profile

## 基础信息
- 时区：UTC+8
- 语言：中文

## 技术栈
- Python
- 使用 uv 进行包管理

## 沟通偏好
- 喜欢简洁直接的沟通
- 优先使用中文回复

## 注意事项
- 深夜不要发送非紧急通知
- 代码规范：遵循项目已有的规范
"""


class UserMemoryStore:
    """管理用户画像 Markdown 文件（只读），所有 Agent 共享。

    文件路径：.soloqueue/USER.md
    """

    def __init__(self, workspace_root: str = "."):
        """初始化用户画像存储。

        Args:
            workspace_root: 工作空间根目录
        """
        self._workspace = Path(workspace_root)
        self._user_md_path = self._workspace / ".soloqueue" / "USER.md"

    @property
    def file_path(self) -> Path:
        """返回用户画像文件路径。"""
        return self._user_md_path

    def read(self) -> str:
        """读取用户画像内容。

        文件不存在时返回空字符串。

        Returns:
            用户画像内容，如果文件不存在则返回空字符串
        """
        if not self._user_md_path.exists():
            logger.info(f"用户画像文件不存在，路径: {self._user_md_path}")
            return ""

        try:
            content = self._user_md_path.read_text(encoding="utf-8")
            logger.debug(f"已读取用户画像，内容长度: {len(content)} 字符")
            return content
        except Exception as e:
            logger.warning(f"读取用户画像失败: {e}")
            return ""

    def exists(self) -> bool:
        """检查用户画像文件是否存在。

        Returns:
            文件是否存在
        """
        return self._user_md_path.exists()

    def create_template(self) -> None:
        """创建用户画像模板文件（如果不存在）。

        仅在文件不存在时创建，不会覆盖已有文件。
        """
        if self._user_md_path.exists():
            logger.debug(f"用户画像文件已存在，跳过创建: {self._user_md_path}")
            return

        self._user_md_path.parent.mkdir(parents=True, exist_ok=True)
        self._user_md_path.write_text(USER_PROFILE_TEMPLATE, encoding="utf-8")
        logger.info(f"已创建用户画像模板: {self._user_md_path}")
