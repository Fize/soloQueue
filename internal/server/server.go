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
	"path/filepath"
	"strings"
	"sync"

	_ "net/http/pprof"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

// Mux is the root HTTP handler.
type Mux struct {
	log               *logger.Logger
	mux               chi.Router
	workDir           string
	registry          *agent.Registry
	supervisorsFn     func() []*agent.Supervisor
	configSvc         *config.GlobalService
	wechatLogin       *wechat.LoginManager
	runtimeMetrics    *RuntimeMetrics
	accessLogger      *httpAccessLogger
	templates         []agent.AgentTemplate
	groupsDir         string // if set, groups are reloaded from disk on each request
	hub               *Hub
	wsTokens          sync.Map
	toolsCfg          *tools.Config
	skillReg          *skill.SkillRegistry
	skillDirs         map[string]string // skill categories → paths, for on-demand reload
	rebuildPrompt     func() error      // rebuilds L1 system prompt after soul/rules edit
	reloadTeamCatalog func() error
	agentsDir         string       // path to ~/.soloqueue/agents directory
	mcpLoader         *mcp.Loader  // MCP config loader for /api/mcp endpoints
	mcpManager        *mcp.Manager // MCP server manager for /api/mcp/available endpoint
	sessionMgr        *session.SessionManager
	l2Store           *session.L2SessionStore // L2 multi-session store (nil if not configured)
	authConfig        config.AuthConfig
	effectiveAuthUser string
	effectiveAuthPass string
	teamstore         *store.Store // team/agent DB store; nil if not backed by SQLite
	onConfigChange    func() error // callback on LLM config update
	simEngine         *simulation.SimulationEngine
	sharedDB          *db.DB // for metric reporting
	workflowStore     *workflow.Store
	workflowRuns      *workflow.RunManager
	distFS            fs.FS
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

// WithWechatLoginManager enables the desktop WeChat QR login endpoints.
func WithWechatLoginManager(manager *wechat.LoginManager) MuxOption {
	return func(m *Mux) { m.wechatLogin = manager }
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
// When set, groups are reloaded from disk on each request (allowedRoots).
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

func WithTeamCatalogReload(fn func() error) MuxOption {
	return func(m *Mux) { m.reloadTeamCatalog = fn }
}

// WithMCPLoader sets the MCP config loader for /api/mcp endpoints.
func WithMCPLoader(loader *mcp.Loader) MuxOption {
	return func(m *Mux) { m.mcpLoader = loader }
}

// WithMCPManager sets the MCP server manager for /api/mcp/available endpoint.
func WithMCPManager(mgr *mcp.Manager) MuxOption {
	return func(m *Mux) { m.mcpManager = mgr }
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
func WithTeamStore(store *store.Store) MuxOption {
	return func(m *Mux) { m.teamstore = store }
}

// WithOnConfigChange sets the callback triggered when database configurations change.
func WithOnConfigChange(fn func() error) MuxOption {
	return func(m *Mux) { m.onConfigChange = fn }
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

// WithSharedDB sets the SQLite DB for token and router stats.
func WithSharedDB(db *db.DB) MuxOption {
	return func(m *Mux) { m.sharedDB = db }
}

// WithWorkflow enables the workflow definition and execution API.
func WithWorkflow(store *workflow.Store, runs *workflow.RunManager) MuxOption {
	return func(m *Mux) { m.workflowStore, m.workflowRuns = store, runs }
}

// WithDistFS overrides the embedded web assets and skills filesystem (useful for testing).
func WithDistFS(fsys fs.FS) MuxOption {
	return func(m *Mux) { m.distFS = fsys }
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

	r.Use(middleware.Recoverer)

	for _, opt := range opts {
		opt(m)
	}

	if m.distFS == nil {
		m.distFS = distFS()
	}

	if m.teamstore == nil && m.workDir != "" {
		m.teamstore = store.NewStore(
			filepath.Join(m.workDir, "groups"),
			filepath.Join(m.workDir, "agents"),
			m.sharedDB,
		)
	}

	// Wire config service hot-reload and on-change callback.
	if m.configSvc != nil {
		if m.onConfigChange != nil {
			m.configSvc.SetOnChange(m.onConfigChange)
		}
		if err := m.configSvc.Watch(); err != nil && log != nil {
			log.WarnContext(context.Background(), logger.CatConfig, "failed to watch config file", "err", err.Error())
		}
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

	// ── Auth middleware (always registered; localhost bypasses, remote requires auth) ──
	m.resolveEffectiveAuth()
	r.Use(m.tokenAuthMiddleware)

	// WebSocket
	r.Get("/ws", m.handleWebSocket)

	// pprof debugging
	r.Get("/debug/*", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/debug/" + chi.URLParam(r, "*")
		http.DefaultServeMux.ServeHTTP(w, r)
	})

	// Session routes
	r.Route("/api/session", func(r chi.Router) {
		r.Get("/", m.handleGetSessionStatus)
		r.Post("/ask", m.handleAskSession)
		// r.Post("/ask/stream", m.handleAskStream) // deprecated: replaced by WebSocket chat_send
		r.Post("/upload", m.handleUploadFile)
		r.Get("/history", m.handleSessionHistory)
		r.Post("/cancel", m.handleCancelSession)
		r.Post("/clear", m.handleClearSession)
		r.Post("/rewind", m.handleRewindSession)
		r.Post("/delete", m.handleDeleteSessionMessages)
		r.Post("/confirm", m.handleConfirmSession)
		r.Get("/list", m.handleListSessions)
		r.Get("/groups", m.handleListL2Groups)
		r.Post("/l2", m.handleCreateL2Session)
		r.Delete("/l2/{id}", m.handleDeleteL2Session)
		r.Get("/l2/{id}/changes", m.handleGetSessionChanges)
	})

	// Auth check
	r.Get("/api/auth/check", m.handleAuthCheck)
	r.Get("/api/auth/token", m.handleGetWSToken)

	// Health check
	r.Get("/healthz", m.handleHealth)

	// Live agents status endpoint
	r.Get("/api/agents/live", m.handleGetLiveAgents)

	// Global rules CRUD for L1 agent
	r.Get("/api/agents/l1/global-rules", m.handleListGlobalRules)
	r.Get("/api/agents/l1/global-rules/{filename}", m.handleGetGlobalRule)
	r.Put("/api/agents/l1/global-rules/{filename}", m.handleSaveGlobalRule)
	r.Delete("/api/agents/l1/global-rules/{filename}", m.handleDeleteGlobalRule)

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
	r.Get("/api/builtin-teams", m.handleListBuiltinTeams)
	r.Post("/api/builtin-teams/install", m.handleInstallBuiltinTeams)
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
			r.Get("/branches", m.handleGetProjectBranches)
		})
	})

	// Config routes
	r.Route("/api/config", func(r chi.Router) {
		r.Get("/", m.handleGetConfig)
		r.Get("/yaml", m.handleGetConfigYAML)

		// File-backed providers & models endpoints
		r.Route("/providers", func(r chi.Router) {
			r.Get("/", m.handleListProviders)
			r.Post("/", m.handleCreateProvider)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", m.handleUpdateProvider)
				r.Delete("/", m.handleDeleteProvider)
				r.Get("/remote-models", m.handleListProviderRemoteModels)
			})
		})

		r.Route("/models", func(r chi.Router) {
			r.Get("/", m.handleListModels)
			r.Post("/", m.handleCreateModel)
			r.Route("/{providerId}", func(r chi.Router) {
				r.Route("/{modelId}", func(r chi.Router) {
					r.Put("/", m.handleUpdateModel)
					r.Delete("/", m.handleDeleteModel)
				})
			})
		})

		r.Route("/model-routes", func(r chi.Router) {
			r.Get("/", m.handleGetModelRoutes)
			r.Put("/", m.handleUpdateModelRoutes)
		})

		r.Route("/tools", func(r chi.Router) {
			r.Get("/", m.handleGetToolsConfig)
			r.Put("/", m.handleUpdateToolsConfig)
		})

		r.Route("/qqbots", func(r chi.Router) {
			r.Get("/", m.handleGetQQBotsConfig)
			r.Put("/", m.handleUpdateQQBotsConfig)
		})

		r.Route("/wechat-bots", func(r chi.Router) {
			r.Get("/", m.handleGetWechatBotsConfig)
			r.Put("/", m.handleUpdateWechatBotsConfig)
			r.Delete("/{accountID}", m.handleDeleteWechatBotConfig)
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

		r.Route("/simulation", func(r chi.Router) {
			r.Get("/", m.handleGetSimulationConfig)
			r.Put("/", m.handleUpdateSimulationConfig)
		})

		r.Route("/speech", func(r chi.Router) {
			r.Get("/", m.handleGetSpeechConfig)
			r.Put("/", m.handleUpdateSpeechConfig)
			r.Get("/status", m.handleGetSpeechStatus)
			r.Post("/install", m.handleInstallSpeech)
		})
	})

	r.Route("/api/channels/wechat", func(r chi.Router) {
		r.Post("/login", m.handleStartWechatLogin)
		r.Get("/login/{sessionID}", m.handleGetWechatLogin)
		r.Post("/login/{sessionID}/verification", m.handleSubmitWechatVerification)
		r.Delete("/login/{sessionID}", m.handleCancelWechatLogin)
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
			r.Post("/auto-update", m.handleToggleSkillAutoUpdate)
		})
	})

	// Cron routes
	r.Route("/api/cron", func(r chi.Router) {
		r.Get("/", m.handleListCronTasks)
		r.Post("/", m.handleCreateCronTask)
		r.Route("/{id}", func(r chi.Router) {
			r.Put("/", m.handleUpdateCronTask)
			r.Delete("/", m.handleDeleteCronTask)
			r.Get("/history", m.handleListCronHistory)
			r.Get("/history/{execID}", m.handleGetCronHistory)
		})
	})

	// Workflow routes. Keep validate and runs before the {name} route so they
	// cannot be interpreted as workflow names.
	r.Route("/api/workflows", func(r chi.Router) {
		r.Get("/", m.handleListWorkflows)
		r.Post("/", m.handleCreateWorkflow)
		r.Post("/validate", m.handleValidateWorkflow)
		r.Get("/builtin", m.handleListBuiltinWorkflows)
		r.Post("/builtin/install", m.handleInstallBuiltinWorkflows)
		r.Route("/{name}", func(r chi.Router) {
			r.Get("/", m.handleGetWorkflow)
			r.Put("/", m.handleUpdateWorkflow)
			r.Delete("/", m.handleDeleteWorkflow)
			r.Get("/runs", m.handleListWorkflowRuns)
			r.Post("/runs", m.handleStartWorkflowRun)
			r.Route("/runs/{runID}", func(r chi.Router) {
				r.Get("/", m.handleGetWorkflowRun)
				r.Post("/cancel", m.handleCancelWorkflowRun)
				r.Post("/pause", m.handlePauseWorkflowRun)
				r.Post("/resume", m.handleResumeWorkflowRun)
				r.Post("/restart", m.handleRestartWorkflowRun)
				r.Post("/abandon", m.handleAbandonWorkflowRun)
				r.Post("/cleanup", m.handleCleanupWorkflowRun)
				r.Get("/events", m.handleListWorkflowRunEvents)
				r.Post("/confirmations/{callID}/resolve", m.handleResolveWorkflowConfirmation)
			})
		})
	})

	// Simulation routes (only if engine is configured)
	if m.simEngine != nil {
		r.Route("/api/simulations", func(r chi.Router) {
			r.Get("/", m.handleListSimulations)
			// 50MB body size limit for create endpoints
			r.With(maxBodyMiddleware(50<<20)).Post("/", m.handleCreateSimulation)
			r.With(maxBodyMiddleware(50<<20)).Post("/from-seed", m.handleCreateFromSeed)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", m.handleGetSimulation)
				r.Put("/", m.handleUpdateSimulation)
				r.Post("/start", m.handleStartSimulation)
				r.Post("/stop", m.handleStopSimulation)
				r.Post("/pause", m.handlePauseSimulation)
				r.Post("/resume", m.handleResumeSimulation)
				r.Post("/step", m.handleStepSimulation)
				r.Post("/agents/{personaId}/ask", m.handleAgentAsk)
				r.Post("/fork", m.handleForkSimulation)
				r.Delete("/", m.handleDeleteSimulation)
				// Generative Agents extensions
				r.Get("/environment", m.handleGetEnvironment)
				r.Get("/agents/{personaId}/plan", m.handleGetAgentPlan)
				r.Get("/agents/{personaId}/memory", m.handleGetAgentMemory)
				r.Get("/agents/{personaId}/reflections", m.handleGetAgentReflections)
			})
		})
	}

	// Stats routes
	r.Route("/api/stats", func(r chi.Router) {
		r.Get("/tokens", m.handleGetTokenStats)
		r.Get("/router", m.handleGetRouterStats)
		r.Get("/classifier", m.handleGetClassifierStats)
		r.Get("/teams", m.handleGetTeams)
		r.Get("/overview", m.handleGetStatsOverview)
		r.Get("/breakdowns", m.handleGetStatsBreakdowns)
		r.Get("/events", m.handleGetStatsEvents)
		r.Get("/filters", m.handleGetStatsFilters)
		r.Get("/activity", m.handleGetStatsActivity)
	})

	// MCP config routes
	r.Get("/api/mcp", m.handleGetMCPConfig)
	r.Patch("/api/mcp", m.handleUpdateMCPConfig)
	r.Get("/api/mcp/available", m.handleGetAvailableMCPServers)
	r.Get("/api/mcp/policies", m.handleGetMCPPolicies)
	r.Put("/api/mcp/policies/{serverName}", m.handleApproveMCPPolicy)
	r.Delete("/api/mcp/policies/{serverName}", m.handleRevokeMCPPolicy)

	// File routes (read-only access to plan directory and team workspaces)
	r.Get("/api/files/content", m.handleGetFileContent)
	r.Get("/api/files/list", m.handleListFiles)
	r.Post("/api/files/toggle-checkbox", m.handleToggleCheckbox)
	r.Post("/api/files/save", m.handleSaveFile)

	// Static file server for embedded web UI (catch-all: only unmatched paths).
	// SPA fallback: if the path does not exist in the embedded FS,
	// serve index.html so React Router can handle client-side routing.
	fsys := m.distFS
	fileServer := http.FileServer(http.FS(fsys))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		// Localhost (127.0.0.1, ::1) bypasses auth.
		if !isLocalhostAccess(r) {
			// Auth not configured — deny all remote access
			if m.effectiveAuthUser == "" {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"remote access not configured"}`))
				return
			}
			// Remote access requires Basic Auth
			user, password, ok := r.BasicAuth()
			if !ok || user != m.effectiveAuthUser || password != m.effectiveAuthPass {
				w.Header().Set("WWW-Authenticate", `Basic realm="SoloQueue Portal"`)
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
		}

		// SPA: serve embedded static files or fallback to index.html
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if info, err := fs.Stat(fsys, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	return m
}

// ServeHTTP implements http.Handler.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

// Close closes any resources held by the Mux (e.g., the access logger).
func (m *Mux) Close() error {
	if m.wechatLogin != nil {
		m.wechatLogin.Close()
	}
	if m.accessLogger != nil {
		return m.accessLogger.Close()
	}
	return nil
}

// ─── Health ─────────────────────────────────────────────────────────────────

func (m *Mux) handleHealth(w http.ResponseWriter, _ *http.Request) {
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "work_dir": m.workDir})
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
			// For file:// and null origins (e.g. Electron desktop app), use *.
			// Access-Control-Allow-Origin: null + Access-Control-Allow-Credentials: true
			// causes CORS failures in Chromium for credentialed cross-origin requests.
			if origin == "null" {
				origin = "*"
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// maxBodyMiddleware limits the request body size to maxBytes.
// Returns 413 Payload Too Large if the body exceeds the limit.
func maxBodyMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
