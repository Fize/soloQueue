package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memoryengine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine/vectorstore"
)

// buildMemory initializes the storage layer: shared SQLite, short-term memory,
// and the memory engine (BM25 + KG + optional vector embedding).
func (bc *buildContext) buildMemory() error {
	// Short-term Memory Manager
	bc.memoryMgr = memory.NewManager(bc.memoryDir, bc.llmClient, bc.fastModelProviderID, bc.fastModelID, bc.log)

	// Shared SQLite DB (already opened in buildMemory() or opened now)
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

	// Memory Engine
	bc.buildMemoryEngine()

	return nil
}

// buildMemoryEngine creates the memory engine with the configured embedding provider.
func (bc *buildContext) buildMemoryEngine() {
	cfg := bc.settings.Embedding

	var emb embedding.Embedder
	var vecStore vectorstore.VectorStore

	provider := cfg.Provider

	switch provider {
	case "openai":
		emb = bc.createOpenAIEmbedder()
	case "none", "":
		// No embedding — pure BM25 + KG
	default:
		bc.log.Warn(logger.CatApp, "build: unknown embedding provider, falling back to none",
			"provider", provider)
	}

	if emb != nil {
		vecStore = vectorstore.NewSQLiteStoreFromDB(bc.sharedDB.DB, &bc.sharedDB.WMu,
			vectorstore.WithTableName("mem_vec"),
			vectorstore.WithLogger(bc.log),
		)
	}

	start := time.Now()
	bc.memoryEngine = memoryengine.New(bc.sharedDB.DB, &bc.sharedDB.WMu, emb, vecStore, bc.log)
	fmt.Fprintf(os.Stderr, "[stderr] build: memory engine ready (has_vector=%v, provider=%s)\n", emb != nil, provider)
	bc.log.Debug(logger.CatApp, "build: memory engine ready",
		"duration", time.Since(start).String(),
		"has_vector", emb != nil,
	)
}

func (bc *buildContext) createOpenAIEmbedder() embedding.Embedder {
	embModel := bc.cfg.DefaultEmbeddingModel()
	if embModel == nil || !embModel.Enabled {
		bc.log.Debug(logger.CatApp, "build: no enabled embedding model, engine runs without vectors")
		return nil
	}
	embProvider := bc.cfg.EmbeddingProviderByID(embModel.ProviderID)
	if embProvider == nil || !embProvider.Enabled {
		return nil
	}

	apiKey := embProvider.APIKey
	if apiKey == "" {
		apiKey = os.Getenv(embProvider.APIKeyEnv)
	}

	client, err := embedding.NewOpenAI(embedding.OpenAIConfig{
		BaseURL:   embProvider.BaseURL,
		APIKey:    apiKey,
		ModelID:   embModel.ID,
		Dimension: embModel.Dimension,
	})
	if err != nil {
		bc.log.Warn(logger.CatApp, "build: failed to create OpenAI embedder, engine runs without vectors", "err", err)
		return nil
	}
	return client
}

// Ensure unused imports are used
var _ = context.Background
