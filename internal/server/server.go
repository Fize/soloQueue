// Package server exposes SoloQueue's REST + WebSocket API using chi router.
//
// Routes:
//
//	GET /healthz → {"status":"ok"}
//	GET /ws → WebSocket for real-time runtime/agent state updates
//	GET /api/plans → list plans
//	GET /api/plans/{id} → get plan detail
//	PUT /api/plans/{id} → update plan
//	DELETE /api/plans/{id} → delete plan
//	PATCH /api/plans/{id}/status → change plan status
//	GET /api/plans/{id}/todos → list todo items
//	PUT /api/plans/{id}/todos/{todoId} → update todo item
//	DELETE /api/plans/{id}/todos/{todoId} → delete todo item
//	PATCH /api/plans/{id}/todos/{todoId}/toggle → toggle completion
//	POST /api/plans/{id}/todos/reorder → reorder todo items
//	GET /api/todos/{id}/dependencies → get dependency graph
//	PUT /api/todos/{id}/dependencies → set dependencies
//	GET /api/agents/{id}/profile → get agent soul & rules
//	PUT /api/agents/{id}/profile → update agent soul & rules
//	GET /api/agents/{id}/config → get agent template YAML + system prompt
//	GET /api/teams → list teams
//	GET /api/skills → list skills (builtin + user)
//	GET /api/files/content?path=<path> → serve file from plan dir or team workspace
//	GET /api/files/list?dir=<path> → list directory contents
//	GET /api/files/info?path=<path> → get file metadata
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/proxy"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Mux is the root HTTP handler.
type Mux struct {
	log            *logger.Logger
	mux            chi.Router
	workDir        string
	registry       *agent.Registry
	supervisorsFn  func() []*agent.Supervisor
	configSvc      *config.GlobalService
	runtimeMetrics *RuntimeMetrics
	accessLogger   *httpAccessLogger
	templates      []agent.AgentTemplate
	groupsDir      string // if set, groups are reloaded from disk on each request
	hub            *Hub
	wsTokens       sync.Map
	toolsCfg       *tools.Config
	skillReg       *skill.SkillRegistry
	skillDirs      map[string]string // skill categories → paths, for on-demand reload
	rebuildPrompt func() error     // rebuilds L1 system prompt after soul/rules edit
	agentsDir     string           // path to ~/.soloqueue/agents directory
	mcpLoader     *mcp.Loader      // MCP config loader for /api/mcp endpoints
	sessionMgr    *session.SessionManager
	l2Store       *session.L2SessionStore // L2 multi-session store (nil if not configured)
	authConfig    config.AuthConfig
	teamstore     *teamstore.Store // team/agent DB store; nil if not backed by SQLite
	onConfigChange func() error     // callback on LLM config update
	proxyManager   *proxy.ProxyManager
	simEngine      *simulation.SimulationEngine
}

// reloadGroups loads groups. If teamstore is available, loads from DB;
// otherwise falls back to parsing groupsDir.
func (m *Mux) reloadGroups() map[string]prompt.GroupFile {
	if m.teamstore != nil {
		teams, err := m.teamstore.ListTeams(context.Background())
		if err != nil {
			if m.log != nil {
				m.log.Warn(logger.CatApp, "reloadGroups list teams failed", "err", err.Error())
			}
			return nil
		}
		groups := make(map[string]prompt.GroupFile, len(teams))
		for _, t := range teams {
			workspaces := make([]prompt.Workspace, 0, len(t.Workspaces))
			for _, w := range t.Workspaces {
				workspaces = append(workspaces, prompt.Workspace{
					Name: w.Name,
					Path: w.Path,
					AutoWork: prompt.AutoWorkConfig{
						Enabled:                 w.AutoWork.Enabled,
						InitialCooldownMinutes:  w.AutoWork.InitialCooldownMinutes,
						PostTaskCooldownMinutes: w.AutoWork.PostTaskCooldownMinutes,
						MaxIntervalsPerDay:      w.AutoWork.MaxIntervalsPerDay,
					},
				})
			}
			groups[t.Name] = prompt.GroupFile{
				Frontmatter: prompt.GroupFrontmatter{
					ID:         t.ID,
					Name:       t.Name,
					Workspaces: workspaces,
					Projects:   t.Projects,
				},
				Body: t.Description,
			}
		}
		return groups
	}

	if m.groupsDir == "" {
		return nil
	}
	groups, err := prompt.LoadGroups(m.groupsDir)
	if err != nil {
		if m.log != nil {
			m.log.Warn(logger.CatApp, "reloadGroups failed", "err", err.Error())
		}
		return nil
	}
	return groups
}

// reloadTemplates loads agent templates from agentsDir on every call.
// Returns nil on error (callers treat nil as "not available").
func (m *Mux) reloadTemplates() []agent.AgentTemplate {
	if m.agentsDir == "" {
		return nil
	}
	templates, err := agent.LoadAgentTemplates(m.agentsDir)
	if err != nil {
		if m.log != nil {
			m.log.Warn(logger.CatApp, "reloadTemplates failed", "err", err.Error())
		}
		return nil
	}
	return templates
}

// MuxOption is a functional option for NewMux.
type MuxOption func(*Mux)

// WithRegistry sets the agent registry for the /api/agents and /api/runtime endpoints.
func WithRegistry(reg *agent.Registry) MuxOption {
	return func(m *Mux) { m.registry = reg }
}

// WithSupervisors sets the function to retrieve supervisors for /api/agents.
func WithSupervisors(fn func() []*agent.Supervisor) MuxOption {
	return func(m *Mux) { m.supervisorsFn = fn }
}

// WithConfigService sets the config service for /api/config endpoints.
func WithConfigService(svc *config.GlobalService) MuxOption {
	return func(m *Mux) { m.configSvc = svc }
}

// WithRuntimeMetrics sets the runtime metrics source for /api/runtime.
func WithRuntimeMetrics(rm *RuntimeMetrics) MuxOption {
	return func(m *Mux) { m.runtimeMetrics = rm }
}

// WithTemplates sets the agent templates for /api/teams.
// Groups are loaded separately via WithGroupsDir for hot-reload support.
func WithTemplates(templates []agent.AgentTemplate) MuxOption {
	return func(m *Mux) {
		m.templates = templates
	}
}

// WithGroupsDir sets the groups directory for hot-reload support.
// When set, groups are reloaded from disk on each request (handleGetFileRoots, allowedRoots).
func WithGroupsDir(dir string) MuxOption {
	return func(m *Mux) { m.groupsDir = dir }
}

// WithHub sets the WebSocket Hub for the /ws endpoint and state broadcasting.
func WithHub(hub *Hub) MuxOption {
	return func(m *Mux) { m.hub = hub }
}

// WithToolsConfig sets the tools configuration for the /api/tools endpoint.
func WithToolsConfig(cfg *tools.Config) MuxOption {
	return func(m *Mux) { m.toolsCfg = cfg }
}

// WithSkillRegistry sets the skill registry for the /api/skills endpoint.
func WithSkillRegistry(reg *skill.SkillRegistry) MuxOption {
	return func(m *Mux) { m.skillReg = reg }
}

// WithSkillDirs sets the skill directories for on-demand reload on each GET /api/skills.
func WithSkillDirs(dirs map[string]string) MuxOption {
	return func(m *Mux) { m.skillDirs = dirs }
}

// WithAgentsDir sets the agents directory for /api/agents/{id}/config.
func WithAgentsDir(dir string) MuxOption {
	return func(m *Mux) { m.agentsDir = dir }
}

// WithPromptRebuild sets the callback that rebuilds the L1 system prompt.
// Called after soul/rules are updated via the API.
func WithPromptRebuild(fn func() error) MuxOption {
	return func(m *Mux) { m.rebuildPrompt = fn }
}

// WithMCPLoader sets the MCP config loader for /api/mcp endpoints.
func WithMCPLoader(loader *mcp.Loader) MuxOption {
	return func(m *Mux) { m.mcpLoader = loader }
}

// WithAuthConfig sets the auth configuration.
// An empty User disables authentication.
func WithAuthConfig(cfg config.AuthConfig) MuxOption {
	return func(m *Mux) {
		m.authConfig = cfg
	}
}

// WithTeamStore sets the team/agent SQLite store for CRUD endpoints.
// When nil, POST/PUT/DELETE team and agent endpoints return 503;
// GET endpoints fall back to file-based loading.
func WithTeamStore(store *teamstore.Store) MuxOption {
	return func(m *Mux) { m.teamstore = store }
}

// WithOnConfigChange sets the callback triggered when database configurations change.
func WithOnConfigChange(fn func() error) MuxOption {
	return func(m *Mux) { m.onConfigChange = fn }
}

// WithProxyManager sets the proxy manager for the /api/proxy endpoints.
func WithProxyManager(pm *proxy.ProxyManager) MuxOption {
	return func(m *Mux) { m.proxyManager = pm }
}

// WithSessionManager sets the session manager for /api/session endpoints.
func WithSessionManager(mgr *session.SessionManager) MuxOption {
	return func(m *Mux) { m.sessionMgr = mgr }
}

// WithL2SessionStore sets the L2 session store for /api/session/l2 endpoints.
func WithL2SessionStore(store *session.L2SessionStore) MuxOption {
	return func(m *Mux) { m.l2Store = store }
}

// WithSimulationEngine sets the simulation engine for /api/simulations endpoints.
func WithSimulationEngine(engine *simulation.SimulationEngine) MuxOption {
	return func(m *Mux) { m.simEngine = engine }
}

// SetHub sets the WebSocket Hub after construction. This is useful when the
// Hub needs a reference to the Mux (circular dependency).
func (m *Mux) SetHub(hub *Hub) {
	m.hub = hub
}

// NewMux creates a new HTTP handler with registered routes.
// workDir is the soloqueue data directory (usually ~/.soloqueue).
// Optional dependencies (registry, configSvc, runtimeMetrics) are passed via MuxOption;
// if nil, their respective endpoints return 503.
func NewMux(workDir string, log *logger.Logger, opts ...MuxOption) *Mux {
	r := chi.NewRouter()

	m := &Mux{
		log:     log,
		mux:     r,
		workDir: workDir,
	}

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(m.corsMiddleware)
	r.Use(m.proxyEntryPointMiddleware)

	// Logging middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.accessLogger != nil {
				m.accessLogger.Middleware(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})
	r.Use(middleware.Recoverer)

	for _, opt := range opts {
		opt(m)
	}

	// HTTP access logger — writes to logs/http/ with 15-day retention, 50MiB max per file
	accessLogDir := filepath.Join(workDir, "logs", "http")
	al, err := newHTTPAccessLogger(accessLogDir, 50, 15)
	if err != nil && log != nil {
		log.ErrorContext(context.Background(), logger.CatHTTP, "failed to create access logger", "err", err.Error())
	}
	if al != nil {
		m.accessLogger = al
		r.Use(al.Middleware)
	}

	// ── Auth middleware (protects all routes below if enabled) ──
	if m.authConfig.User != "" {
		r.Use(m.tokenAuthMiddleware)
	}

	// WebSocket
	r.Get("/ws", m.handleWebSocket)

	// Session routes
	r.Route("/api/session", func(r chi.Router) {
		r.Get("/", m.handleGetSessionStatus)
		r.Post("/ask", m.handleAskSession)
		r.Post("/ask/stream", m.handleAskStream)
		r.Post("/upload", m.handleUploadFile)
		r.Get("/history", m.handleSessionHistory)
		r.Post("/cancel", m.handleCancelSession)
		r.Post("/clear", m.handleClearSession)
		r.Post("/confirm", m.handleConfirmSession)
		r.Get("/list", m.handleListSessions)
		r.Get("/groups", m.handleListL2Groups)
		r.Post("/l2", m.handleCreateL2Session)
		r.Delete("/l2/{id}", m.handleDeleteL2Session)
	})

	// Auth check
	r.Get("/api/auth/check", m.handleAuthCheck)
	r.Get("/api/auth/token", m.handleGetWSToken)

	// Health check
	r.Get("/healthz", m.handleHealth)

	// Live agents status endpoint
	r.Get("/api/agents/live", m.handleGetLiveAgents)



	// Agent config/profile routes (specific sub-paths registered before {name} catch-all)
	r.Get("/api/agents/{id}/profile", m.handleGetAgentProfile)
	r.Put("/api/agents/{id}/profile", m.handleUpdateAgentProfile)
	r.Get("/api/agents/{id}/config", m.handleGetAgentConfig)
	r.Put("/api/agents/{id}/config", m.handleUpdateAgentConfig)

	// Agent CRUD (DB-backed; registered after specific sub-paths to avoid conflicts)
	r.Get("/api/agents", m.handleListAgents)
	r.Post("/api/agents", m.handleCreateAgent)
	r.Get("/api/agents/{name}", m.handleGetAgent)
	r.Put("/api/agents/{name}", m.handleUpdateAgent)
	r.Delete("/api/agents/{name}", m.handleDeleteAgent)

	// Teams CRUD (DB-backed with file fallback for GET)
	r.Get("/api/teams", m.handleListTeams)
	r.Post("/api/teams", m.handleCreateTeam)
	r.Get("/api/teams/{name}", m.handleGetTeam)
	r.Put("/api/teams/{name}", m.handleUpdateTeam)
	r.Delete("/api/teams/{name}", m.handleDeleteTeam)

	// Projects CRUD (DB-backed)
	r.Route("/api/projects", func(r chi.Router) {
		r.Get("/", m.handleListProjects)
		r.Post("/", m.handleCreateProject)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", m.handleGetProject)
			r.Put("/", m.handleUpdateProject)
			r.Delete("/", m.handleDeleteProject)
		})
	})

	// Config routes
	r.Route("/api/config", func(r chi.Router) {
		r.Get("/", m.handleGetConfig)
		r.Get("/toml", m.handleGetConfigToml)

		// DB-backed providers & models endpoints
		r.Route("/providers", func(r chi.Router) {
			r.Get("/", m.handleListProviders)
			r.Post("/", m.handleCreateProvider)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", m.handleUpdateProvider)
				r.Delete("/", m.handleDeleteProvider)
			})
		})

		r.Route("/models", func(r chi.Router) {
			r.Get("/", m.handleListModels)
			r.Post("/", m.handleCreateModel)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", m.handleUpdateModel)
				r.Delete("/", m.handleDeleteModel)
			})
		})

		r.Route("/default-models", func(r chi.Router) {
			r.Get("/", m.handleGetDefaultModels)
			r.Put("/", m.handleUpdateDefaultModels)
		})

		r.Route("/tools", func(r chi.Router) {
			r.Get("/", m.handleGetToolsConfig)
			r.Put("/", m.handleUpdateToolsConfig)
		})

		r.Route("/qqbot", func(r chi.Router) {
			r.Get("/", m.handleGetQQBotConfig)
			r.Put("/", m.handleUpdateQQBotConfig)
		})

		r.Route("/lspmcp", func(r chi.Router) {
			r.Get("/", m.handleGetLSPMCPConfig)
			r.Put("/", m.handleUpdateLSPMCPConfig)
		})

		r.Route("/embedding", func(r chi.Router) {
			r.Get("/", m.handleGetEmbeddingConfig)
			r.Put("/", m.handleUpdateEmbeddingConfig)
		})

		r.Route("/session", func(r chi.Router) {
			r.Get("/", m.handleGetSessionConfig)
			r.Put("/", m.handleUpdateSessionConfig)
		})
	})

	// Tools & Skills routes
	r.Get("/api/tools", m.handleListTools)
	r.Route("/api/skills", func(r chi.Router) {
		r.Get("/", m.handleListSkills)
		r.Post("/", m.handleImportSkill)
		r.Get("/store", m.handleListStoreSkills)
		r.Post("/install", m.handleInstallSkill)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", m.handleGetSkillDetail)
			r.Put("/", m.handleUpdateSkill)
			r.Delete("/", m.handleDeleteSkill)
			r.Get("/files", m.handleGetSkillFiles)
			r.Post("/toggle", m.handleToggleSkill)
		})
	})

	// Proxy routes
	r.Route("/api/proxy", func(r chi.Router) {
		r.Get("/", m.handleListProxies)
		r.Post("/", m.handleCreateProxy)
		r.Route("/{id}", func(r chi.Router) {
			r.Delete("/", m.handleDeleteProxy)
		})
	})

	// Cron routes
	r.Route("/api/cron", func(r chi.Router) {
		r.Get("/", m.handleListCronTasks)
		r.Post("/", m.handleCreateCronTask)
		r.Route("/{id}", func(r chi.Router) {
			r.Put("/", m.handleUpdateCronTask)
			r.Delete("/", m.handleDeleteCronTask)
		})
	})

	// Simulation routes (only if engine is configured)
	if m.simEngine != nil {
		r.Route("/api/simulations", func(r chi.Router) {
			r.Get("/", m.handleListSimulations)
			r.Post("/", m.handleCreateSimulation)
			r.Post("/from-seed", m.handleCreateFromSeed)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", m.handleGetSimulation)
				r.Put("/", m.handleUpdateSimulation)
				r.Post("/start", m.handleStartSimulation)
				r.Post("/stop", m.handleStopSimulation)
				r.Post("/agents/{personaId}/ask", m.handleAgentAsk)
				r.Post("/fork", m.handleForkSimulation)
				r.Delete("/", m.handleDeleteSimulation)
			})
		})
	}

	// MCP config routes
	r.Get("/api/mcp", m.handleGetMCPConfig)
	r.Patch("/api/mcp", m.handleUpdateMCPConfig)

	// File routes (read-only access to plan directory and team workspaces)
	r.Get("/api/files/roots", m.handleGetFileRoots)
	r.Get("/api/files/content", m.handleGetFileContent)
	r.Get("/api/files/list", m.handleListFiles)
	r.Get("/api/files/info", m.handleGetFileInfo)
	r.Post("/api/files/toggle-checkbox", m.handleToggleCheckbox)

	// Static file server for embedded web UI (catch-all: only unmatched paths).
	// SPA fallback: if the path does not exist in the embedded FS,
	// serve index.html so React Router can handle client-side routing.
	fsys := distFS()
	fileServer := http.FileServer(http.FS(fsys))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		// Serve SoloQueue's own embedded static files first to prevent them from being hijacked by the proxy.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if info, err := fs.Stat(fsys, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// If it's a request from SoloQueue's own UI, don't proxy it.
		if r.Header.Get("X-SoloQueue-Request") == "true" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		targetProxyID := m.detectProxyID(r)
		if targetProxyID != "" && m.proxyManager != nil && m.proxyManager.HasProxy(targetProxyID) {
			m.proxyManager.CachePath(r.URL.Path, targetProxyID)
			m.serveReverseProxy(w, r, targetProxyID)
			return
		}

		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	return m
}

func (m *Mux) detectProxyID(r *http.Request) string {
	// 1. Direct query parameter
	if id := r.URL.Query().Get("soloqueue_proxy"); id != "" {
		return id
	}

	// 2. Referer-based detection
	referer := r.Header.Get("Referer")
	if referer != "" {
		if refURL, err := url.Parse(referer); err == nil {
			if id := refURL.Query().Get("soloqueue_proxy"); id != "" {
				return id
			} else if m.proxyManager != nil {
				if id := m.proxyManager.GetCachedProxy(refURL.Path); id != "" {
					return id
				}
			}
		}
	}

	// 3. Fallback cookie for WebSockets and relative sub-resource requests
	isWebsocket := strings.ToLower(r.Header.Get("Upgrade")) == "websocket" || r.Header.Get("Sec-WebSocket-Key") != ""
	isHTML := strings.Contains(r.Header.Get("Accept"), "text/html")
	if isWebsocket || (!isHTML && r.URL.Path != "/") {
		if cookie, err := r.Cookie("soloqueue_active_proxy"); err == nil && cookie.Value != "" {
			return cookie.Value
		}
	}

	return ""
}

func (m *Mux) proxyEntryPointMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If it's a request from SoloQueue's own UI, bypass proxying.
		if r.Header.Get("X-SoloQueue-Request") == "true" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if request matches an existing static asset in the embedded filesystem.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			fsys := distFS()
			if info, err := fs.Stat(fsys, path); err == nil && !info.IsDir() {
				// Serve static file normally, bypass proxy.
				next.ServeHTTP(w, r)
				return
			}
		}

		proxyID := m.detectProxyID(r)
		if proxyID != "" && m.proxyManager != nil && m.proxyManager.HasProxy(proxyID) {
			m.proxyManager.CachePath(r.URL.Path, proxyID)
			m.serveReverseProxy(w, r, proxyID)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ServeHTTP implements http.Handler.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

// Close closes any resources held by the Mux (e.g., the access logger).
func (m *Mux) Close() error {
	if m.accessLogger != nil {
		return m.accessLogger.Close()
	}
	return nil
}

// ─── Health ─────────────────────────────────────────────────────────────────

func (m *Mux) handleHealth(w http.ResponseWriter, _ *http.Request) {
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}



// ─── Helpers ────────────────────────────────────────────────────────────────

func (m *Mux) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(payload)
	if err != nil {
		if m.log != nil {
			m.log.ErrorContext(context.Background(), logger.CatHTTP, "writeJSON marshal failed", "err", err.Error())
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}

func (m *Mux) logError(ctx context.Context, msg string, err error) {
	if m.log == nil {
		return
	}
	m.log.LogError(ctx, logger.CatHTTP, msg, err)
}

// corsMiddleware handles CORS for the Web UI dev server.
func (m *Mux) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
