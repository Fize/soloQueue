from types import SimpleNamespace

import asyncio

from soloqueue.orchestration.orchestrator import Orchestrator
from soloqueue.orchestration.signals import ControlSignal, SignalType


class _DummyMemory:
    def start_session(self, *_args, **_kwargs):
        return None

    def end_session(self, *_args, **_kwargs):
        return None

    def save_error(self, *_args, **_kwargs):
        return None


class _DummyRegistry:
    def __init__(self):
        self._leader = SimpleNamespace(
            group="investment",
            node_id="investment__leader",
            is_leader=True,
            name="leader",
        )
        self._worker = SimpleNamespace(
            group="investment",
            node_id="investment__worker",
            is_leader=False,
            name="worker",
        )

    def get_agent_by_name(self, name, _group=None):
        if name == "leader":
            return self._leader
        if name == "worker":
            return self._worker
        return None

    def get_agent_by_node_id(self, node_id):
        if node_id == self._leader.node_id:
            return self._leader
        if node_id == self._worker.node_id:
            return self._worker
        return None


def _build_orchestrator() -> Orchestrator:
    orch = Orchestrator(_DummyRegistry(), workspace_root=".")
    orch._get_memory_manager = lambda _group: _DummyMemory()  # type: ignore[method-assign]
    return orch


def test_run_no_initial_starting_status_event():
    """run() 不应在入口处额外发送 starting 事件。"""
    orch = _build_orchestrator()
    events = []

    orch._execute_frame = lambda *_args, **_kwargs: ControlSignal(  # type: ignore[method-assign]
        type=SignalType.RETURN,
        result="ok",
    )

    result = asyncio.run(orch.run("leader", "hello", step_callback=events.append))

    assert result == "ok"
    starting = [e for e in events if e.get("type") == "agent_status" and e.get("status") == "starting"]
    completed = [e for e in events if e.get("type") == "agent_status" and e.get("status") == "completed"]

    assert starting == []
    assert len(completed) == 1


def test_delegate_path_no_duplicate_starting_status_event():
    """委派路径不应额外发送 delegate starting 事件。"""
    orch = _build_orchestrator()
    events = []

    signals = [
        ControlSignal(
            type=SignalType.DELEGATE,
            target_agent="worker",
            instruction="do task",
            tool_call_id="call_1",
        ),
        ControlSignal(type=SignalType.RETURN, result="worker done"),
        ControlSignal(type=SignalType.RETURN, result="leader done"),
    ]

    def _fake_execute(*_args, **_kwargs):
        return signals.pop(0)

    orch._execute_frame = _fake_execute  # type: ignore[method-assign]
    orch._resolve_agent = lambda *_args, **_kwargs: orch.registry.get_agent_by_name("worker")  # type: ignore[method-assign]
    orch._check_permission = lambda *_args, **_kwargs: True  # type: ignore[method-assign]

    result = asyncio.run(orch.run("leader", "hello", step_callback=events.append))

    assert result == "leader done"
    starting = [e for e in events if e.get("type") == "agent_status" and e.get("status") == "starting"]
    completed = [e for e in events if e.get("type") == "agent_status" and e.get("status") == "completed"]

    assert starting == []
    assert len(completed) == 2
