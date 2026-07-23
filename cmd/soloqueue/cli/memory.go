package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

func MemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Audit and maintain long-term memory",
	}
	cmd.AddCommand(memoryAuditCmd())
	cmd.AddCommand(memoryCleanupCmd())
	return cmd
}

func memoryAuditCmd() *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Print a read-only long-term memory audit",
		RunE: func(cmd *cobra.Command, _ []string) error {
			engine, closeDB, _, err := openMemoryEngineReadOnly(dbPath)
			if err != nil {
				return err
			}
			defer closeDB()
			report, err := engine.Audit(cmd.Context())
			if err != nil {
				return err
			}
			return writeJSON(cmd, report)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "database path (defaults to ~/.soloqueue/soloqueue.db)")
	return cmd
}

func memoryCleanupCmd() *cobra.Command {
	var dbPath, manifestPath, projectRoot string
	var apply bool
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Plan or apply reversible legacy-memory cleanup",
		RunE: func(cmd *cobra.Command, _ []string) error {
			engine, closeDB, resolvedDB, err := openMemoryEngine(dbPath)
			if err != nil {
				return err
			}
			defer closeDB()

			manifest, err := engine.PlanLegacyCleanup(cmd.Context(), projectRoot)
			if err != nil {
				return err
			}
			if manifestPath == "" {
				manifestPath = filepath.Join(
					filepath.Dir(resolvedDB),
					"backups",
					"memory-cleanup-"+time.Now().Format("20060102-150405")+".json",
				)
			}
			if err := writeManifest(manifestPath, manifest); err != nil {
				return err
			}

			result := map[string]any{
				"apply":         apply,
				"manifest_path": manifestPath,
				"decisions":     len(manifest.Decisions),
			}
			if len(manifest.Decisions) == 0 {
				result["already_clean"] = true
				return writeJSON(cmd, result)
			}
			if apply {
				backupPath, err := backupDatabase(cmd.Context(), engine, resolvedDB)
				if err != nil {
					return err
				}
				if err := engine.ApplyLegacyCleanup(cmd.Context(), manifest); err != nil {
					return err
				}
				report, err := engine.Audit(cmd.Context())
				if err != nil {
					return err
				}
				result["backup_path"] = backupPath
				result["audit"] = report
			}
			return writeJSON(cmd, result)
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "database path (defaults to ~/.soloqueue/soloqueue.db)")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "cleanup manifest path")
	cmd.Flags().StringVar(&projectRoot, "project-root", "", "project root used to scope matching legacy memories")
	cmd.Flags().BoolVar(&apply, "apply", false, "apply the manifest after creating a database backup")
	return cmd
}

func openMemoryEngine(path string) (*memoryengine.Engine, func(), string, error) {
	path, err := resolveMemoryDBPath(path)
	if err != nil {
		return nil, nil, "", err
	}
	db, err := sqlitedb.Open(path)
	if err != nil {
		return nil, nil, "", err
	}
	engine := memoryengine.New(db.DB, &db.WMu, nil, nil, nil)
	return engine, func() { _ = db.Close() }, path, nil
}

func openMemoryEngineReadOnly(path string) (*memoryengine.Engine, func(), string, error) {
	path, err := resolveMemoryDBPath(path)
	if err != nil {
		return nil, nil, "", err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_foreign_keys=ON")
	if err != nil {
		return nil, nil, "", err
	}
	var mu sync.Mutex
	engine := memoryengine.New(db, &mu, nil, nil, nil)
	return engine, func() { _ = db.Close() }, path, nil
}

func resolveMemoryDBPath(path string) (string, error) {
	if path == "" {
		workDir, err := config.DefaultWorkDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(workDir, "soloqueue.db")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return path, nil
}

func writeManifest(path string, manifest memoryengine.CleanupManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write cleanup manifest: %w", err)
	}
	return nil
}

func backupDatabase(ctx context.Context, engine *memoryengine.Engine, dbPath string) (string, error) {
	backupDir := filepath.Join(filepath.Dir(dbPath), "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(backupDir, "soloqueue-before-memory-cleanup-"+time.Now().Format("20060102-150405")+".db")
	if err := engine.VacuumInto(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
