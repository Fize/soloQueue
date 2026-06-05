package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/embedding"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
	"github.com/xiaobaitu/soloqueue/internal/vectorstore"
)

// buildMemory initializes the storage layer: shared SQLite, short-term memory,
// and permanent memory (embedding + vectorstore + scheduler).
func (bc *buildContext) buildMemory() error {
	// ── Short-term Memory Manager ─────────────────────────────────
	bc.memoryMgr = memory.NewManager(bc.memoryDir, bc.llmClient, bc.fastModelProviderID, bc.fastModelID, bc.log)

	// ── Shared SQLite DB ──────────────────────────────────────────────────
	embStart := time.Now()
	if bc.sharedDB == nil {
		sharedDBPath := filepath.Join(bc.workDir, "permanent_memory", "entries.db")
		sharedDB, sharedDBErr := sqlitedb.Open(sharedDBPath)
		if sharedDBErr != nil {
			return fmt.Errorf("open shared sqlite db: %w", sharedDBErr)
		}
		bc.sharedDB = sharedDB
		bc.log.Debug(logger.CatApp, "build: sqlite opened", "duration", time.Since(embStart).String())
	}
	if bc.teamstore == nil {
		bc.teamstore = teamstore.NewStore(filepath.Join(bc.workDir, "groups"), filepath.Join(bc.workDir, "agents"), bc.sharedDB)
		// Migrate direct workspaces to projects table
		if err := bc.teamstore.MigrateWorkspacesToProjects(context.Background()); err != nil {
			bc.log.Warn(logger.CatApp, "failed to migrate team workspaces to projects", "err", err.Error())
		}
	}

	// ── Permanent Memory Manager ──────────────────────────────────────────
	bc.buildPermanentMemory()

	return nil
}

// buildPermanentMemory initializes permanent memory components.
// Uses early return to avoid deep nesting levels.
func (bc *buildContext) buildPermanentMemory() {
	if !bc.settings.Embedding.Enabled {
		return
	}

	embModel := bc.cfg.DefaultEmbeddingModel()
	if embModel == nil || !embModel.Enabled {
		return
	}

	embProvider := bc.cfg.EmbeddingProviderByID(embModel.ProviderID)
	if embProvider == nil || !embProvider.Enabled {
		return
	}

	apiKey := embProvider.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(embProvider.APIKeyEnv)
	}

	embClient, embErr := embedding.NewOpenAI(embedding.OpenAIConfig{
		BaseURL:   embProvider.BaseURL,
		APIKey:    apiKey,
		ModelID:   embModel.ID,
		Dimension: embModel.Dimension,
	})
	if embErr != nil {
		bc.log.Warn(logger.CatApp, "permanent memory: failed to create embedder", "err", embErr)
		return
	}

	normFlag := embModel.Normalize
	store := vectorstore.NewSQLiteStoreFromDB(bc.sharedDB.DB, &bc.sharedDB.WMu,
		vectorstore.WithLogger(bc.log),
	)

	minSim := bc.settings.Embedding.MinSimilarity
	if minSim == 0 {
		minSim = 0.65
	}
	permBuildStart := time.Now()
	var summarizer permanent.Summarizer
	if bc.llmClient != nil {
		summarizer = permanent.SummarizeFunc(func(ctx context.Context, req permanent.SummarizeRequest) (permanent.SummarizeResponse, error) {
			var msgs []agent.LLMMessage
			for _, m := range req.Messages {
				msgs = append(msgs, agent.LLMMessage{
					Role:    m.Role,
					Content: m.Content,
				})
			}
			resp, err := bc.llmClient.Chat(ctx, agent.LLMRequest{
				ProviderID:  req.ProviderID,
				Model:       req.Model,
				Messages:    msgs,
				MaxTokens:   req.MaxTokens,
				Temperature: req.Temperature,
			})
			if err != nil {
				return permanent.SummarizeResponse{}, err
			}
			return permanent.SummarizeResponse{
				Content: resp.Content,
			}, nil
		})
	}
	permanentMgr := permanent.NewManager(store, embClient, summarizer, bc.fastModelProviderID, bc.fastModelID, bc.memoryDir, bc.log, minSim, normFlag)
	permScheduler := permanent.NewScheduler(permanentMgr, bc.log, func(msg string) {
		bc.log.Error(logger.CatApp, msg)
	})
	permCtx, cancel := context.WithCancel(context.Background())

	bc.permanentMgr = permanentMgr
	bc.permScheduler = permScheduler
	bc.permCancel = cancel

	go func() {
		defer func() {
			if r := recover(); r != nil {
				bc.log.Error(logger.CatApp, "permScheduler goroutine panic recovered",
					"panic", fmt.Sprintf("%v", r))
			}
		}()
		permScheduler.Run(permCtx)
	}()

	bc.log.Debug(logger.CatApp, "build: permanent memory ready", "duration", time.Since(permBuildStart).String())
}
