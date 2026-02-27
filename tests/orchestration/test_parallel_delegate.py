"""并行委派 (delegate_parallel) 功能测试。"""

import asyncio
from types import SimpleNamespace
from unittest.mock import patch

import pytest

from soloqueue.orchestration.orchestrator import Orchestrator
from soloqueue.orchestration.signals import ControlSignal, SignalType, ParallelDelegateTarget


# ── Fixtures ──────────────────────────────────────────────────────────────


class _DummyMemory:
    def start_session(self, *a, **kw):
        return None

    def end_session(self, *a, **kw):
        return None

    def save_error(self, *a, **kw):
        return None


def _make_agent(name, group="team", is_leader=False, sub_agents=None, **extra):
    return SimpleNamespace(
        name=name,
        node_id=f"{group}__{name}",
        group=group,
        is_leader=is_leader,
        description=f"{name} agent",
        model=None,
        tools=[],
        skills=[],
        color=None,
        sub_agents=sub_agents or [],
        **extra,
    )


class _TestRegistry:
    def __init__(self, agents: list):
        self._by_id = {a.node_id: a for a in agents}
        self._by_name = {a.name: a for a in agents}
        self.groups = {}

    def get_agent_by_name(self, name, _group=None):
        return self._by_name.get(name)

    def get_agent_by_node_id(self, node_id):
        return self._by_id.get(node_id)


def _build_orch(agents: list) -> Orchestrator:
    reg = _TestRegistry(agents)
    orch = Orchestrator(reg, workspace_root=".")
    orch._get_memory_manager = lambda _g: _DummyMemory()
    return orch


# ── Tests ─────────────────────────────────────────────────────────────────


def test_parallel_delegate_basic():
    """Leader 并行委派两个子 Agent，结果聚合后返回。"""
    leader = _make_agent("leader", is_leader=True, sub_agents=["analyst", "researcher"])
    analyst = _make_agent("analyst")
    researcher = _make_agent("researcher")

    orch = _build_orch([leader, analyst, researcher])

    # 模拟信号序列
    call_count = {"n": 0}

    def _fake_execute(frame, **kw):
        call_count["n"] += 1
        if call_count["n"] == 1:
            # Leader 第一步：发起并行委派
            return ControlSignal(
                type=SignalType.DELEGATE_PARALLEL,
                tool_call_id="call_p1",
                parallel_delegates=[
                    ParallelDelegateTarget(
                        target_agent="analyst",
                        instruction="analyze data",
                        tool_call_id="call_p1",
                    ),
                    ParallelDelegateTarget(
                        target_agent="researcher",
                        instruction="research topic",
                        tool_call_id="call_p1",
                    ),
                ],
            )
        else:
            # Leader 第二步：综合分析后返回
            return ControlSignal(type=SignalType.RETURN, result="综合分析完成")

    orch._execute_frame = _fake_execute

    # Mock _run_sub_agent 以避免真正调用 LLM
    def _mock_run_sub(agent_config, instruction, session_id, step_callback):
        return f"{agent_config.name} result for: {instruction}"

    orch._run_sub_agent = _mock_run_sub
    orch._resolve_agent = lambda name, _cur: orch.registry.get_agent_by_name(name)
    orch._check_permission = lambda *a, **kw: True

    result = asyncio.run(orch.run("leader", "请分析"))
    assert result == "综合分析完成"
    # Leader 被调用两次：第一次触发并行委派，第二次接收结果后返回
    assert call_count["n"] == 2


def test_parallel_delegate_results_injected():
    """验证并行结果以 ToolMessage 注入 Leader memory。"""
    leader = _make_agent("leader", is_leader=True, sub_agents=["a1", "a2"])
    a1 = _make_agent("a1")
    a2 = _make_agent("a2")
    orch = _build_orch([leader, a1, a2])

    injected_messages = []
    call_count = {"n": 0}

    def _fake_execute(frame, **kw):
        call_count["n"] += 1
        if call_count["n"] == 1:
            return ControlSignal(
                type=SignalType.DELEGATE_PARALLEL,
                tool_call_id="call_p2",
                parallel_delegates=[
                    ParallelDelegateTarget("a1", "task1", "call_p2"),
                    ParallelDelegateTarget("a2", "task2", "call_p2"),
                ],
            )
        else:
            # 收集注入到 frame.memory 中的 ToolMessage
            for msg in frame.memory:
                if hasattr(msg, "name") and msg.name == "delegate_parallel":
                    injected_messages.append(msg.content)
            return ControlSignal(type=SignalType.RETURN, result="done")

    orch._execute_frame = _fake_execute
    orch._run_sub_agent = lambda cfg, instr, *a: f"{cfg.name}: ok"
    orch._resolve_agent = lambda name, _c: orch.registry.get_agent_by_name(name)
    orch._check_permission = lambda *a, **kw: True

    asyncio.run(orch.run("leader", "go"))

    assert len(injected_messages) == 2
    assert any("a1" in m for m in injected_messages)
    assert any("a2" in m for m in injected_messages)


def test_parallel_delegate_retry_on_failure():
    """单个子 Agent 失败后自动重试 1 次。"""
    leader = _make_agent("leader", is_leader=True, sub_agents=["flaky"])
    flaky = _make_agent("flaky")
    orch = _build_orch([leader, flaky])

    attempt = {"count": 0}

    def _flaky_sub(cfg, instr, *a):
        attempt["count"] += 1
        if attempt["count"] == 1:
            raise RuntimeError("temporary failure")
        return "recovered"

    call_count = {"n": 0}

    def _fake_execute(frame, **kw):
        call_count["n"] += 1
        if call_count["n"] == 1:
            return ControlSignal(
                type=SignalType.DELEGATE_PARALLEL,
                tool_call_id="call_r1",
                parallel_delegates=[
                    ParallelDelegateTarget("flaky", "do it", "call_r1"),
                ],
            )
        else:
            return ControlSignal(type=SignalType.RETURN, result="ok")

    orch._execute_frame = _fake_execute
    orch._run_sub_agent = _flaky_sub
    orch._resolve_agent = lambda name, _c: orch.registry.get_agent_by_name(name)
    orch._check_permission = lambda *a, **kw: True

    result = asyncio.run(orch.run("leader", "go"))
    assert result == "ok"
    # 第一次失败 + 第二次重试成功 = 2 次调用
    assert attempt["count"] == 2


def test_parallel_delegate_all_fail():
    """所有子 Agent 都失败（重试后仍失败），结果包含错误信息。"""
    leader = _make_agent("leader", is_leader=True, sub_agents=["bad"])
    bad = _make_agent("bad")
    orch = _build_orch([leader, bad])

    def _always_fail(cfg, instr, *a):
        raise RuntimeError("permanent failure")

    injected = []
    call_count = {"n": 0}

    def _fake_execute(frame, **kw):
        call_count["n"] += 1
        if call_count["n"] == 1:
            return ControlSignal(
                type=SignalType.DELEGATE_PARALLEL,
                tool_call_id="call_f1",
                parallel_delegates=[
                    ParallelDelegateTarget("bad", "do it", "call_f1"),
                ],
            )
        else:
            for msg in frame.memory:
                if hasattr(msg, "name") and msg.name == "delegate_parallel":
                    injected.append(msg.content)
            return ControlSignal(type=SignalType.RETURN, result="handled error")

    orch._execute_frame = _fake_execute
    orch._run_sub_agent = _always_fail
    orch._resolve_agent = lambda name, _c: orch.registry.get_agent_by_name(name)
    orch._check_permission = lambda *a, **kw: True

    result = asyncio.run(orch.run("leader", "go"))
    assert result == "handled error"
    assert len(injected) == 1
    assert "Error" in injected[0]


def test_parallel_delegate_permission_denied():
    """跨组委派被拒绝时返回错误信息。"""
    leader = _make_agent("leader", group="A", is_leader=True, sub_agents=["worker"])
    worker = _make_agent("worker", group="B", is_leader=False)
    orch = _build_orch([leader, worker])

    call_count = {"n": 0}

    def _fake_execute(frame, **kw):
        call_count["n"] += 1
        if call_count["n"] == 1:
            return ControlSignal(
                type=SignalType.DELEGATE_PARALLEL,
                tool_call_id="call_perm",
                parallel_delegates=[
                    ParallelDelegateTarget("worker", "task", "call_perm"),
                ],
            )
        else:
            return ControlSignal(type=SignalType.RETURN, result="denied handled")

    orch._execute_frame = _fake_execute
    orch._resolve_agent = lambda name, _c: orch.registry.get_agent_by_name(name)
    # 真实权限检查：跨组非 Leader-Leader 会被拒绝
    # orch._check_permission 使用默认实现

    result = asyncio.run(orch.run("leader", "go"))
    assert result == "denied handled"


def test_parallel_events_emitted():
    """验证 parallel_started 和 parallel_completed 事件被正确发送。"""
    leader = _make_agent("leader", is_leader=True, sub_agents=["w1"])
    w1 = _make_agent("w1")
    orch = _build_orch([leader, w1])
    events = []

    call_count = {"n": 0}

    def _fake_execute(frame, **kw):
        call_count["n"] += 1
        if call_count["n"] == 1:
            return ControlSignal(
                type=SignalType.DELEGATE_PARALLEL,
                tool_call_id="call_ev",
                parallel_delegates=[
                    ParallelDelegateTarget("w1", "work", "call_ev"),
                ],
            )
        else:
            return ControlSignal(type=SignalType.RETURN, result="done")

    orch._execute_frame = _fake_execute
    orch._run_sub_agent = lambda cfg, instr, *a: "result"
    orch._resolve_agent = lambda name, _c: orch.registry.get_agent_by_name(name)
    orch._check_permission = lambda *a, **kw: True

    asyncio.run(orch.run("leader", "go", step_callback=events.append))

    event_types = [e["type"] for e in events]
    assert "parallel_started" in event_types
    assert "parallel_completed" in event_types
