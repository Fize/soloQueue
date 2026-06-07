package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
)

type personasFile struct {
	Personas []simulation.Persona `toml:"personas"`
}

// SimulateCmd returns the 'simulate' cobra command.
func SimulateCmd(version string) *cobra.Command {
	var (
		topic            string
		personas         string
		maxActions       int
		maxWallClockMs   int
		triggerPolicy    string
		minSpeakInterval int
		reportOut        string
		dbPath           string
	)

	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Run a multi-agent event-driven simulation",
		Long: `Run a multi-agent evolutionary simulation where AI agents with distinct
personas interact asynchronously in event-driven mode.

Example:
  soloqueue simulate --topic "Should we use Rust or Go?" --personas personas.toml --db ./sim.db`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			if personas == "" {
				return fmt.Errorf("--personas is required")
			}

			var pf personasFile
			data, err := os.ReadFile(personas)
			if err != nil {
				return fmt.Errorf("read personas file: %w", err)
			}
			if err := toml.Unmarshal(data, &pf); err != nil {
				return fmt.Errorf("parse personas file: %w", err)
			}
			if len(pf.Personas) == 0 {
				return fmt.Errorf("no personas defined in %s", personas)
			}

			simConfig := simulation.SimulationConfig{
				Topic:              topic,
				Personas:           pf.Personas,
				MaxActions:         maxActions,
				MaxWallClockMs:     maxWallClockMs,
				TriggerPolicy:      triggerPolicy,
				MinSpeakIntervalMs: minSpeakInterval,
			}

			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return fmt.Errorf("work dir: %w", err)
			}

			cfg, err := config.Init(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}

			log, err := runtime.InitLogger(workDir, cfg, true)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}

			rt, err := runtime.Build(workDir, cfg, log, nil, false)
			if err != nil {
				return fmt.Errorf("build runtime: %w", err)
			}
			defer rt.Shutdown()

			engine := rt.SimulationEngine
			if engine == nil {
				return fmt.Errorf("simulation engine is not available")
			}

			// Override DB path if specified
			if dbPath != "" {
				engine.SetDBPath(dbPath)
			}

			id, err := engine.Create(simConfig)
			if err != nil {
				return fmt.Errorf("create simulation: %w", err)
			}

			fmt.Printf("Simulation: %s\n", id)
			fmt.Printf("Topic: %s  |  Personas: %d  |  Max actions: %d  |  Trigger: %s\n\n",
				topic, len(pf.Personas), simConfig.MaxActions, simConfig.TriggerPolicy)

			ctx := context.Background()
			events, err := engine.Start(ctx, id)
			if err != nil {
				return fmt.Errorf("start: %w", err)
			}

			started := time.Now()
			for ev := range events {
				switch ev.Type {
				case "agent_message":
					if msg, ok := ev.Data.(simulation.RoundMessage); ok {
						fmt.Printf("[%s] (%s): %s\n", msg.AgentName, msg.Type, truncForCLI(msg.Content, 150))
					}
				case "simulation_end":
					elapsed := time.Since(started).Round(time.Second)
					fmt.Printf("\nSimulation complete in %s\n", elapsed)
					if data, ok := ev.Data.(map[string]any); ok {
						fmt.Printf("Total actions: %v\n", data["total_actions"])
					}
				case "error":
					fmt.Printf("ERROR: %s\n", ev.Error)
				}
			}

			state, err := engine.Get(id)
			if err != nil {
				return fmt.Errorf("get final state: %w", err)
			}

			if state.Report != "" {
				fmt.Printf("\n═══ FINAL REPORT ═══\n%s\n", state.Report)
				if reportOut != "" {
					os.WriteFile(reportOut, []byte(state.Report), 0o644)
					fmt.Printf("Report saved to: %s\n", reportOut)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&topic, "topic", "t", "", "Discussion topic (required)")
	cmd.Flags().StringVarP(&personas, "personas", "p", "", "Path to personas TOML file (required)")
	cmd.Flags().IntVar(&maxActions, "max-actions", 15, "Maximum total agent actions")
	cmd.Flags().IntVar(&maxWallClockMs, "max-wall-clock", 300000, "Max wall clock time in ms")
	cmd.Flags().StringVar(&triggerPolicy, "trigger", "selective", "Trigger policy: reactive|selective")
	cmd.Flags().IntVar(&minSpeakInterval, "min-speak-interval", 2000, "Min ms between agent responses")
	cmd.Flags().StringVarP(&reportOut, "output", "o", "", "Save final report to file")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path for persistence")

	return cmd
}

func truncForCLI(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
