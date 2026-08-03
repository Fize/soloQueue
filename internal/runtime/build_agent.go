package runtime

import (
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
)

// buildAgentInfra initializes the Agent Registry, Factory, Compactor, and Task Router.
func (bc *buildContext) buildAgentInfra() {
	// Initialize tools configuration
	toolsCfg := bc.settings.Tools.ToToolsConfigWithSandbox(bc.settings.Sandbox)
	toolsCfg.MemoryEngine = bc.memoryEngine
	toolsCfg.PlanDir = bc.planDir
	toolsCfg.RuntimeManager = bc.runtimeMgr
	bc.toolsCfg = toolsCfg

	// ── Agent Registry + Factory ──────────────────────────────────────────────
	bc.agentRegistry = agent.NewRegistry(bc.log)
	bc.modelResolver = BuildModelResolver(bc.cfg)
	bc.exploreDir = prompt.ExploreDir(bc.workDir)

	bc.agentFactory = agent.NewDefaultFactory(
		bc.agentRegistry, bc.llmClient, bc.toolsCfg, bc.log,
		agent.WithModelResolver(bc.modelResolver),
		agent.WithDefaultModelID(bc.defaultModel.ID),
		agent.WithTemplates(bc.allTemplates),
		agent.WithGroups(bc.groups),
		agent.WithWorkDir(bc.workDir),
		agent.WithBypassConfirm(bc.bypassConfirm),
		agent.WithMCPManager(bc.mcpMgr),
		agent.WithSkillRegistry(bc.skillReg),
		agent.WithExploreDir(bc.exploreDir),
		agent.WithTeamStore(bc.teamstore),
	)

	// ── L2 Supervisors ────────────────────────────────────────────────────────
	bc.supervisors = []*agent.Supervisor{} // empty slice

	// ── Compactor (context compression engine) ────────────────────────────
	compactorModel := bc.cfg.DefaultClassifierModel()
	if compactorModel == nil {
		compactorModel = bc.defaultModel
	}
	compactorModelID := compactorModel.APIModel
	if compactorModelID == "" {
		compactorModelID = compactorModel.ID
	}
	bc.compactorInstance = NewLLMCompactor(
		NewAgentChatClient(bc.llmClient),
		compactorModel.ProviderID,
		compactorModelID,
		WithLogger(bc.log),
	)

	bc.tokenizer = ctxwin.NewTokenizer()

	// ── Task Router Classifier ───────────────────────────────────────────────
	classifierModelConfig := bc.cfg.DefaultClassifierModel()
	if classifierModelConfig == nil {
		classifierModelConfig = bc.defaultModel
	}
	classifierModel := classifierModelConfig.APIModel
	if classifierModel == "" {
		classifierModel = classifierModelConfig.ID
	}
	classifierConfig := router.DefaultClassifierConfig()
	classifier := router.NewDefaultClassifier(classifierConfig, bc.llmClient, classifierModelConfig.ProviderID, classifierModel, bc.log)
	bc.taskRouter = router.NewRouter(classifier, bc.cfg, bc.log)
}
