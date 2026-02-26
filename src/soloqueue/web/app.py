import os
import re
import asyncio
from pathlib import Path
import contextlib
from fastapi import FastAPI, Request, WebSocket, WebSocketDisconnect
from fastapi.templating import Jinja2Templates
from fastapi.staticfiles import StaticFiles
from fastapi.responses import HTMLResponse, JSONResponse

from soloqueue.core.logger import logger
from soloqueue.core.registry import Registry
from soloqueue.core.loaders import (
    AgentLoader, GroupLoader, SkillLoader,
    AgentSchema, GroupSchema, SkillSchema
)
from soloqueue.web.config import web_config
from soloqueue.web.utils.colors import get_agent_color
from soloqueue.core.security.approval import set_webui_connected, get_webui_approval
from soloqueue.web.websocket.schemas import WriteActionResponse, parse_websocket_message
from soloqueue.web.websocket.handlers import (
    set_write_action_websocket,
    add_connection,
    remove_connection,
    get_active_connection_count,
)
from soloqueue.web.api.artifacts import router as artifacts_router
from soloqueue.orchestration.orchestrator import Orchestrator
import glob
import json

# Name validation pattern
NAME_PATTERN = re.compile(r'^[a-zA-Z0-9_-]+$')

# --- Helpers ---
def get_recent_logs(limit=5):
    try:
        log_files = glob.glob("logs/*.jsonl")
        if not log_files:
            return []
        latest_log = max(log_files, key=os.path.getmtime)
        
        logs = []
        with open(latest_log, 'r') as f:
            lines = f.readlines()
            for line in lines[-limit:]:
                try:
                    entry = json.loads(line)
                    rec = entry.get("record", {})
                    msg = rec.get("message", entry.get("text", str(entry)))
                    level = rec.get("level", {}).get("name", "INFO")
                    time_struct = rec.get("time", {}) 
                    time = time_struct.get("repr", "")[:19] # Truncate if too long
                    
                    logs.append({"level": level, "message": msg, "time": time})
                except Exception:
                    continue
        return list(reversed(logs))
    except Exception:
        return []


# Global Registry Instance
registry = Registry()

# Main event loop reference (for cross-thread async calls)
_main_loop = None

def get_main_loop():
    """Get the main asyncio event loop for cross-thread coroutine submission."""
    return _main_loop

# Lifespan context manager for startup/shutdown events
@contextlib.asynccontextmanager
async def lifespan(app: FastAPI):
    global _main_loop
    _main_loop = asyncio.get_running_loop()
    # Startup
    logger.info(f"Starting SoloQueue Web on {web_config.HOST}:{web_config.PORT}")
    registry.initialize()
    yield
    # Shutdown
    _main_loop = None

# Initialize FastAPI App
app = FastAPI(
    title="SoloQueue Web",
    description="Web Interface for SoloQueue Agent Swarm",
    version="0.1.0",
    lifespan=lifespan
)

# Setup Paths
BASE_DIR = Path(__file__).resolve().parent
TEMPLATES_DIR = BASE_DIR / "templates"
STATIC_DIR = BASE_DIR / "static"

# Mount Static Files
if not STATIC_DIR.exists():
    STATIC_DIR.mkdir(parents=True)
app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")

# Setup Templates
templates = Jinja2Templates(directory=str(TEMPLATES_DIR))

# Include API routers
app.include_router(artifacts_router)

# Global Registry Instance (already defined above)



@app.get("/", response_class=HTMLResponse)
async def dashboard(request: Request):
    """Render the dashboard page."""
    # Compute stats
    team_count = len(registry.groups)
    agent_count = len(registry.agents)
    skill_count = len(registry.skills)
    
    logs = get_recent_logs()
    
    return templates.TemplateResponse(
        "dashboard.html", 
        {
            "request": request,
            "active_page": "dashboard",
            "stats": {
                "teams": team_count,
                "agents": agent_count,
                "skills": skill_count,
            },
            "logs": logs,
        }
    )

@app.get("/teams", response_class=HTMLResponse)
async def list_teams(request: Request):
    """Render the teams list page."""
    teams = list(registry.groups.values())
    return templates.TemplateResponse(
        "teams.html", 
        {
            "request": request, 
            "active_page": "teams",
            "teams": teams
        }
    )
    
@app.get("/agents", response_class=HTMLResponse)
async def list_agents(request: Request):
    """Render the agents list page."""
    agents = list(registry.agents.values())
    return templates.TemplateResponse(
        "agents.html", 
        {
            "request": request, 
            "active_page": "agents",
            "agents": agents
        }
    )

@app.get("/skills", response_class=HTMLResponse)
async def list_skills(request: Request):
    """Render the skills list page."""
    skills = list(registry.skills.values()) if hasattr(registry, 'skills') else []
    
    return templates.TemplateResponse(
        "skills.html", 
        {
            "request": request, 
            "active_page": "skills",
            "skills": skills
        }
    )

# --- NEW routes (must be before /{name} routes) ---

@app.get("/teams/new", response_class=HTMLResponse)
async def new_team_page(request: Request):
    return templates.TemplateResponse(
        "team_new.html",
        {"request": request, "active_page": "teams"}
    )

@app.get("/agents/new", response_class=HTMLResponse)
async def new_agent_page(request: Request):
    group = request.query_params.get("group", "")
    groups = list(registry.groups.keys())
    return templates.TemplateResponse(
        "agent_new.html",
        {"request": request, "active_page": "agents", "default_group": group, "groups": groups}
    )

@app.get("/skills/new", response_class=HTMLResponse)
async def new_skill_page(request: Request):
    return templates.TemplateResponse(
        "skill_new.html",
        {"request": request, "active_page": "skills"}
    )

# --- Detail and Edit routes ---

@app.get("/skills/{name}", response_class=HTMLResponse)
async def skill_detail(request: Request, name: str):
    """Render the skill detail page."""
    skill = registry.skills.get(name)
    if not skill:
        return HTMLResponse(content="Skill not found", status_code=404)
        
    return templates.TemplateResponse(
        "skill_detail.html", 
        {
            "request": request, 
            "active_page": "skills",
            "skill": skill
        }
    )

@app.get("/skills/{name}/edit", response_class=HTMLResponse)
async def edit_skill_page(request: Request, name: str):
    skill = registry.skills.get(name)
    if not skill:
         return HTMLResponse(content="Skill not found", status_code=404)
    skill_dict = skill.model_dump()
    return templates.TemplateResponse(
        "skill_edit.html", 
        {"request": request, "skill": skill, "skill_json": skill_dict, "active_page": "skills"}
    )

@app.put("/api/skills/{name}")
async def update_skill(name: str, skill_data: SkillSchema):
    if skill_data.name != name:
         return {"status": "error", "message": "Renaming skills is not supported via this API."}
         
    loader = SkillLoader()
    try:
        loader.save(skill_data)
        registry.skills[name] = skill_data
        return {"status": "success", "skill": skill_data}
    except Exception as e:
        return {"status": "error", "message": str(e)}

@app.get("/agents/{name}", response_class=HTMLResponse)
async def agent_detail(request: Request, name: str):
    """Render the agent detail page."""
    # Search in all loaded agents (across groups)
    # The registry stores by node_id (group__name) usually, but let's check structure.
    # Registry.agents is Dict[str, AgentSchema]. Key is node_id? 
    # Usually registry.agents_by_node. Let's iterate to find by simple name or node_id.
    
    target_agent = None
    if name in registry.agents: 
        target_agent = registry.agents[name]
    else:
        # Fallback search by simple name (might be ambiguous but okay for UI)
        for agent in registry.agents.values():
            if agent.name == name:
                target_agent = agent
                break
    
    if not target_agent:
        return user_error_page(request, "Agent not found", f"Agent '{name}' does not exist.")

    return templates.TemplateResponse(
        "agent_detail.html", 
        {
            "request": request, 
            "agent": target_agent,
            "active_page": "agents"
        }
    )

@app.get("/agents/{name}/edit", response_class=HTMLResponse)
async def agent_edit(request: Request, name: str):
    """Render the agent edit page."""
    # (Reuse logic to find agent)
    target_agent = None
    if name in registry.agents: 
        target_agent = registry.agents[name]
    else:
        for agent in registry.agents.values():
            if agent.name == name:
                target_agent = agent
                break
    
    if not target_agent: # type: ignore
        return user_error_page(request, "Agent not found", f"Agent '{name}' does not exist.")

    # Convert Pydantic to dict for JSON serialization in template
    agent_dict = target_agent.model_dump() # type: ignore
    
    return templates.TemplateResponse(
        "agent_edit.html", 
        {
            "request": request, 
            "agent": target_agent, # For Jinja rendering
            "agent_json": agent_dict, # For Alpine x-data
            "active_page": "agents"
        }
    )


@app.get("/teams/{name}", response_class=HTMLResponse)
async def team_detail(request: Request, name: str):
    """Render the team detail page."""
    team = registry.groups.get(name)
    if not team:
         return user_error_page(request, "Team not found", f"Team '{name}' does not exist.")
         
    # Find members
    members = [a for a in registry.agents.values() if a.group == name]
    
    # Find team leader
    leader_name = ""
    for m in members:
        if m.is_leader:
            leader_name = m.name
            break
    if not leader_name and members:
        leader_name = members[0].name
    
    # Build agent color mapping for frontend
    agent_colors = {m.name: get_agent_color(m.name, getattr(m, 'color', None)) for m in members}

    return templates.TemplateResponse(
        "team_detail.html", 
        {
            "request": request, 
            "team": team,
            "members": members,
            "leader_name": leader_name,
            "agent_colors": agent_colors,
            "active_page": "teams"
        }
    )

@app.get("/teams/{name}/edit", response_class=HTMLResponse)
async def edit_team_page(request: Request, name: str):
    team = registry.groups.get(name)
    if not team:
         return HTMLResponse("Team not found", status_code=404)
    team_dict = team.model_dump()
    return templates.TemplateResponse(
        "team_edit.html", 
        {"request": request, "team": team, "team_json": team_dict, "active_page": "teams"}
    )

@app.put("/api/teams/{name}")
async def update_team(name: str, group_data: GroupSchema):
    if group_data.name != name:
        return {"status": "error", "message": "Renaming teams is not supported via this API."}
    
    loader = GroupLoader()
    try:
        loader.save(group_data)
        registry.groups[name] = group_data
        return {"status": "success", "team": group_data}
    except Exception as e:
        return {"status": "error", "message": str(e)}




# ... existing code ...

def user_error_page(request: Request, title: str, message: str):
    """Helper to render error page (using base.html for now)."""
    return templates.TemplateResponse(
        "base.html",
        {
            "request": request,
            "error": True,
            "title": title,
            "message": message
        },
        status_code=404
    )

@app.put("/api/agents/{name}")
async def update_agent(name: str, update_data: AgentSchema):
    """Update an agent configuration."""
    if update_data.name != name:
        pass
        
    loader = AgentLoader()
    try:
        loader.save(update_data)
        
        # Update Memory Registry
        if name in registry.agents:
            registry.agents[name] = update_data
        node_id = f"{update_data.group}__{update_data.name}"
        registry.agents_by_node[node_id] = update_data
        
        return {"status": "success", "agent": update_data}
        
    except Exception as e:
        return {"status": "error", "message": str(e)}


# === CREATE APIs ===

@app.post("/api/teams")
async def create_team(request: Request):
    """Create a new team."""
    body = await request.json()
    name = body.get("name", "").strip()
    
    if not name or not NAME_PATTERN.match(name):
        return JSONResponse({"status": "error", "message": "Invalid name. Use only letters, numbers, hyphens, underscores."}, status_code=400)
    if name in registry.groups:
        return JSONResponse({"status": "error", "message": f"Team '{name}' already exists."}, status_code=400)
    
    try:
        group = GroupSchema(
            name=name,
            description=body.get("description", ""),
            shared_context=body.get("shared_context", ""),
        )
        loader = GroupLoader()
        loader.save(group)
        registry.groups[name] = group
        return {"status": "success", "name": name}
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)


@app.post("/api/agents")
async def create_agent(request: Request):
    """Create a new agent."""
    body = await request.json()
    name = body.get("name", "").strip()
    
    if not name or not NAME_PATTERN.match(name):
        return JSONResponse({"status": "error", "message": "Invalid name. Use only letters, numbers, hyphens, underscores."}, status_code=400)
    if name in registry.agents:
        return JSONResponse({"status": "error", "message": f"Agent '{name}' already exists."}, status_code=400)
    
    try:
        agent = AgentSchema(
            name=name,
            description=body.get("description", ""),
            model=body.get("model") or None,
            reasoning=body.get("reasoning", False),
            group=body.get("group") or None,
            is_leader=body.get("is_leader", False),
            tools=body.get("tools", []),
            sub_agents=body.get("sub_agents", []),
            memory=body.get("memory") or None,
            color=body.get("color") or None,
        )
        agent.system_prompt = body.get("system_prompt", "")
        
        loader = AgentLoader()
        loader.save(agent)
        registry.agents[name] = agent
        if agent.group:
            registry.agents_by_node[f"{agent.group}__{name}"] = agent
        return {"status": "success", "name": name}
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)


@app.post("/api/skills")
async def create_skill(request: Request):
    """Create a new skill."""
    body = await request.json()
    name = body.get("name", "").strip()
    
    if not name or not NAME_PATTERN.match(name):
        return JSONResponse({"status": "error", "message": "Invalid name. Use only letters, numbers, hyphens, underscores."}, status_code=400)
    if name in registry.skills:
        return JSONResponse({"status": "error", "message": f"Skill '{name}' already exists."}, status_code=400)
    
    try:
        skill = SkillSchema(
            name=name,
            description=body.get("description", ""),
            allowed_tools=body.get("allowed_tools", []),
            disable_model_invocation=body.get("disable_model_invocation", False),
            subagent=body.get("subagent") or None,
            arguments=body.get("arguments") or None,
        )
        skill.content = body.get("content", "")
        
        loader = SkillLoader()
        loader.save(skill)
        registry.skills[name] = skill
        return {"status": "success", "name": name}
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)


# === DELETE APIs ===

@app.delete("/api/teams/{name}")
async def delete_team(name: str):
    """Delete a team."""
    if name not in registry.groups:
        return JSONResponse({"status": "error", "message": f"Team '{name}' not found."}, status_code=404)
    
    try:
        loader = GroupLoader()
        loader.delete(name)
        del registry.groups[name]
        return {"status": "success"}
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)


@app.delete("/api/agents/{name}")
async def delete_agent(name: str):
    """Delete an agent."""
    if name not in registry.agents:
        return JSONResponse({"status": "error", "message": f"Agent '{name}' not found."}, status_code=404)
    
    try:
        agent = registry.agents[name]
        loader = AgentLoader()
        loader.delete(name)
        
        # Clean up registry
        del registry.agents[name]
        node_id = f"{agent.group}__{agent.name}" if agent.group else agent.name
        registry.agents_by_node.pop(node_id, None)
        
        return {"status": "success"}
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)


@app.delete("/api/skills/{name}")
async def delete_skill(name: str):
    """Delete a skill."""
    if name not in registry.skills:
        return JSONResponse({"status": "error", "message": f"Skill '{name}' not found."}, status_code=404)
    
    try:
        loader = SkillLoader()
        loader.delete(name)
        del registry.skills[name]
        return {"status": "success"}
    except Exception as e:
        return JSONResponse({"status": "error", "message": str(e)}, status_code=500)



@app.websocket("/ws/chat")
async def chat_endpoint(websocket: WebSocket):
    await websocket.accept()
    try:
        # Create a fresh orchestrator for this session (or reuse if stateful)
        # For now, stateless per request is safer.
        # But we want memory persistence across messages?
        # Orchestrator handles memory via MemoryManager(disk). So recreating it is fine, 
        # as long as session ID is consistent.
        # Currently Orchestrator.run() starts a NEW session every time?
        # Let's check orchestrator.py:52 -> primary_memory.start_session()
        # Yes, it resets session.
        # So multi-turn chat is NOT supported by current Orchestrator implementation.
        # It's one-shot. That matches "SoloQueue" CLI behavior.
        
        orch = Orchestrator(registry)
        
        while True:
            data = await websocket.receive_json()
            if data.get("type") == "chat":
                user_msg = data.get("content", "")
                
                import asyncio
                import queue
                
                target_agent = data.get("target_agent", "")
                
                # 1. Determine Entry Agent
                if target_agent:
                    entry_agent = target_agent
                else:
                    entry_agent = "leader" # Default fallback
                    for name, agent in registry.agents.items():
                        if agent.is_leader:
                            entry_agent = name
                            break
                
                # 2. Setup Streaming Queue
                msg_queue = queue.Queue()
                
                def step_callback(event):
                    msg_queue.put(event)
                
                loop = asyncio.get_event_loop()
                
                # 3. Run Orchestrator in Thread (Sync -> Async Bridge)
                future = loop.run_in_executor(
                    None, 
                    lambda: orch.run(entry_agent, user_msg, step_callback=step_callback)
                )
                
                # 4. Stream Results (batch drain for reliability)
                while True:
                    # Drain all available events from queue
                    events_sent = 0
                    while True:
                        try:
                            event = msg_queue.get_nowait()
                            await websocket.send_json(event)
                            events_sent += 1
                        except queue.Empty:
                            break
                    
                    # If no events were sent and future is done, exit
                    if events_sent == 0 and future.done():
                        # Final drain to catch any last-moment events
                        while not msg_queue.empty():
                            try:
                                event = msg_queue.get_nowait()
                                await websocket.send_json(event)
                            except queue.Empty:
                                break
                        break
                    
                    if events_sent == 0:
                        await asyncio.sleep(0.05)
                
                # 5. Wait for completion (completed status is emitted from orchestrator RETURN event)
                try:
                    await future
                except Exception as e:
                     await websocket.send_json({
                        "type": "error",
                        "content": f"Execution Error: {str(e)}"
                    })
                    
    except WebSocketDisconnect:
        logger.info("Chat WebSocket client disconnected")


@app.websocket("/ws/write-action")
async def write_action_endpoint(websocket: WebSocket):
    """WebSocket endpoint for write-action confirmations."""
    await websocket.accept()

    # Update connection tracking
    add_connection(websocket)
    set_write_action_websocket(websocket)
    set_webui_connected(True)

    try:
        while True:
            # Receive JSON message
            data = await websocket.receive_json()

            # Parse and validate message
            try:
                message = parse_websocket_message(data)
            except ValueError as e:
                await websocket.send_json({
                    "type": "error",
                    "content": f"Invalid message: {str(e)}"
                })
                continue

            # Handle write_action_response messages
            if isinstance(message, WriteActionResponse):
                # Forward to WebUIApproval instance
                webui_approval = get_webui_approval()
                if webui_approval:
                    success = webui_approval.submit_webui_response(message.id, message.approved)
                    if not success:
                        await websocket.send_json({
                            "type": "error",
                            "content": f"No pending request with id: {message.id}"
                        })
                else:
                    await websocket.send_json({
                        "type": "error",
                        "content": "WebUI approval not available"
                    })
            else:
                # Ignore other message types (could be heartbeats, etc.)
                pass

    except WebSocketDisconnect:
        logger.info("Write-action WebSocket client disconnected")
    finally:
        # Clean up connection tracking
        remove_connection(websocket)
        if get_active_connection_count() == 0:
            set_webui_connected(False)


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(
        "soloqueue.web.app:app",
        host=web_config.HOST,
        port=web_config.PORT,
        reload=web_config.DEBUG,
        log_level="info" if web_config.DEBUG else "warning"
    )


