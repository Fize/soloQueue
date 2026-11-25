package runtime

import (
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/router/laya"
)

// buildAgentInfra initializes the Agent Registry, Factory, Compactor, and Task Router.
func (bc *buildContext) buildAgentInfra() {
	// Initialize tools configuration
	toolsCfg := bc.settings.Tools.ToToolsConfig()
	toolsCfg.WorkDir = bc.workDir
	toolsCfg.MemoryEngine = bc.memoryEngine
	toolsCfg.PlanDir = bc.planDir
	toolsCfg.Executor = bc.executor
	bc.toolsCfg = toolsCfg

	// ── Agent Registry + Factory ──────────────────────────────────────────────
	bc.agentRegistry = agent.NewRegistry(bc.log)
	bc.modelResolver = BuildModelResolver(bc.cfg)
	bc.exploreDir = prompt.ExploreDir(bc.workDir)

	bc.agentFactory = agent.NewDefaultFactory(
		bc.agentRegistry, bc.llmClient, bc.toolsCfg, bc.log,
		agent.WithModelResolver(bc.modelResolver),
		agent.WithDefaultModelID(bc.defaultModel.ProviderID+":"+bc.defaultModel.ID),
		agent.WithTemplates(bc.allTemplates),
		agent.WithGroups(bc.groups),
		agent.WithWorkDir(bc.workDir),
		agent.WithMCPManager(bc.mcpMgr),
		agent.WithSkillRegistry(bc.skillReg),
		agent.WithExploreDir(bc.exploreDir),
		agent.WithTeamStore(bc.teamstore),
		agent.WithMemoryEngine(bc.memoryEngine),
	)
	if bc.sharedDB != nil {
		bc.agentFactory.ApplyOption(agent.WithSkillInvocationStats(skill.NewSQLiteInvocationStats(bc.sharedDB)))
	}

	// ── L2 Supervisors ────────────────────────────────────────────────────────
	bc.supervisors = []*agent.Supervisor{} // empty slice

	// ── Compactor (context compression engine) ────────────────────────────
	compactorModelID := bc.defaultModel.APIModel
	if compactorModelID == "" {
		compactorModelID = bc.defaultModel.ID
	}
	bc.compactorInstance = NewLLMCompactor(
		NewAgentChatClient(bc.llmClient),
		bc.defaultModel.ProviderID,
		compactorModelID,
		WithLogger(bc.log),
	)

	bc.tokenizer = ctxwin.NewTokenizer()

	// ── Task Router Classifier ───────────────────────────────────────────────
	classifierModel := bc.defaultModel.APIModel
	if classifierModel == "" {
		classifierModel = bc.defaultModel.ID
	}
	classifierConfig := router.DefaultClassifierConfig()
	// NewDefaultClassifier normalizes an empty engine in both startup and reload paths.
	classifierConfig.Engine = router.Engine(bc.settings.Classifier.Engine)
	bc.classifierManager = laya.New(bc.workDir, bc.log)
	classifier := router.NewDefaultClassifier(classifierConfig, bc.llmClient, bc.defaultModel.ProviderID, classifierModel, bc.classifierManager, bc.log)
	bc.taskRouter = router.NewRouter(classifier, bc.log)
	bc.classifierManager.Apply(layaConfig(bc.settings.Classifier))
}

// layaConfig maps the persisted classifier settings onto the local manager.
func layaConfig(c config.ClassifierConfig) laya.Config {
	return laya.Config{
		Enabled:  c.Engine == string(router.EngineLaya),
		ModelDir: c.ModelDir,
		Device:   "cpu",
	}
}
