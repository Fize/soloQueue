package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

// SkillsCmd groups skill-registry inspection commands.
func SkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Inspect the skill registry and invocation telemetry",
	}
	cmd.AddCommand(skillsReportCmd())
	return cmd
}

// skillsReportCmd prints the skill.BuildGovernanceReport output.
func skillsReportCmd() *cobra.Command {
	var workDir, dbPath string
	var days int
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print a skill governance report (never-invoked skills, description quality)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if workDir == "" {
				wd, err := config.DefaultWorkDir()
				if err != nil {
					return fmt.Errorf("resolve work dir: %w", err)
				}
				workDir = wd
			}
			if dbPath == "" {
				path, err := resolveMemoryDBPath("")
				if err != nil {
					return err
				}
				dbPath = path
			}
			if days <= 0 {
				days = 30
			}

			reg := skill.NewSkillRegistry()
			skills, err := skill.LoadSkillsFromDirs(map[string]string{
				"user": filepath.Join(workDir, "skills"),
			})
			if err != nil {
				return fmt.Errorf("load skills: %w", err)
			}
			for _, s := range skills {
				_ = reg.Register(s)
			}

			d, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer d.Close()

			report, err := skill.BuildGovernanceReport(reg, skill.NewSQLiteInvocationStats(d), time.Now().Add(-time.Duration(days)*24*time.Hour))
			if err != nil {
				return fmt.Errorf("build report: %w", err)
			}
			return writeJSON(cmd, report)
		},
	}
	cmd.Flags().StringVar(&workDir, "work-dir", "", "soloqueue work directory (defaults to ~/.soloqueue)")
	cmd.Flags().StringVar(&dbPath, "db", "", "database path (defaults to ~/.soloqueue/soloqueue.db)")
	cmd.Flags().IntVar(&days, "days", 30, "invocation lookback window in days")
	return cmd
}
