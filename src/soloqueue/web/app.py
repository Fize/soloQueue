import os
from pathlib import Path
import contextlib
from fastapi import FastAPI, Request, WebSocket, WebSocketDisconnect
from fastapi.templating import Jinja2Templates
from fastapi.staticfiles import StaticFiles
from fastapi.responses import HTMLResponse

from soloqueue.core.registry import Registry
from soloqueue.core.loaders import (
    AgentLoader, GroupLoader, SkillLoader,
    AgentSchema, GroupSchema, SkillSchema
)
from soloqueue.web.config import web_config
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

def get_system_health():
    try:
        load = os.getloadavg()[0] # 1 min load
        cpu = min(int(load * 50), 100)
        
        if os.path.exists('/proc/meminfo'):
             with open('/proc/meminfo', 'r') as f:
                lines = f.readlines()
                total = int(lines[0].split()[1])
                avail = int(lines[2].split()[1])
                used = total - avail
                mem = int((used / total) * 100)
        else:
             mem = 0
             
        return {"cpu": cpu, "memory": mem}
    except Exception:
        return {"cpu": 0, "memory": 0}

# Global Registry Instance
registry = Registry()

# Lifespan context manager for startup/shutdown events
@contextlib.asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup
    print(f"Starting SoloQueue Web on {web_config.HOST}:{web_config.PORT}")
    registry.initialize()
    yield
    # Shutdown would go here if needed
    # print("Shutting down SoloQueue Web...")

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
    health = get_system_health()
    
    return templates.TemplateResponse(
        "dashboard.html", 
        {
            "request": request,
            "active_page": "dashboard",
            "stats": {
                "teams": team_count,
                "agents": agent_count,
                "skills": skill_count,
                "tokens": "0" # Placeholder
            },
            "logs": logs,
            "health": health
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
    # Skills might be loaded on demand or preloaded.
    skills = list(registry.skills.values()) if hasattr(registry, 'skills') else []
    
    return templates.TemplateResponse(
        "skills.html", 
        {
            "request": request, 
            "active_page": "skills",
            "skills": skills
        }
    )

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
         
    return templates.TemplateResponse(
        "skill_edit.html", 
        {"request": request, "skill": skill.model_dump(), "active_page": "skills"}
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
    
    return templates.TemplateResponse(
        "team_detail.html", 
        {
            "request": request, 
            "team": team,
            "members": members,
            "leader_name": leader_name,
            "active_page": "teams"
        }
    )

@app.get("/teams/{name}/edit", response_class=HTMLResponse)
async def edit_team_page(request: Request, name: str):
    team = registry.groups.get(name)
    if not team:
         return HTMLResponse("Team not found", status_code=404)
    return templates.TemplateResponse(
        "team_edit.html", 
        {"request": request, "team": team.model_dump(), "active_page": "teams"}
    )

@app.get("/api/teams/{name}")
async def get_team_api(name: str):
    """Get team configuration as JSON."""
    team = registry.groups.get(name)
    if not team:
         return {"status": "error", "message": "Team not found"}
    return team

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
        # Validation: Name change not supported in this endpoint
        pass
        
    loader = AgentLoader()
    try:
        loader.save(update_data)
        
        # Update Memory Registry
        if name in registry.agents:
            registry.agents[name] = update_data
        # Update node_id mapping (group__name)
        node_id = f"{update_data.group}__{update_data.name}"
        registry.agents_by_node[node_id] = update_data
        
        return {"status": "success", "agent": update_data}
        
    except Exception as e:
        return {"status": "error", "message": str(e)}


@app.get("/chat", response_class=HTMLResponse)
async def chat_page(request: Request):
    """Render the chat/debug page."""
    agents = []
    if hasattr(registry, 'agents'):
        agents = list(registry.agents.values())
        
    return templates.TemplateResponse(
        "chat.html", 
        {
            "request": request, 
            "active_page": "chat",
            "agents": agents
        }
    )

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
                
                # 4. Stream Results
                while not future.done() or not msg_queue.empty():
                    try:
                        # Non-blocking get
                        event = msg_queue.get_nowait()
                        # Forward event to frontend
                        await websocket.send_json(event)
                    except queue.Empty:
                        if future.done():
                            break
                        await asyncio.sleep(0.05)
                
                # 5. Final Result
                try:
                    result = await future
                    # Send final response if not already covered by events
                    await websocket.send_json({
                        "type": "response",
                        "content": str(result)
                    })
                except Exception as e:
                     await websocket.send_json({
                        "type": "error",
                        "content": f"Execution Error: {str(e)}"
                    })
                    
    except WebSocketDisconnect:
        print("Client disconnected")


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
        print("Write-action WebSocket client disconnected")
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


