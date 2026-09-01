package config

// DefaultSettings returns hardcoded default configuration settings (lowest priority)
// Corresponds to DEFAULT_CONFIGS and DEFAULT_LLM_CONFIGS in typescript seeds.ts
func DefaultSettings() Settings {
	return Settings{
		Session: SessionConfig{
			TimelineMaxFileMB: 50,
		},
		Log: LogConfig{
			Level:   "info",
			Console: false,
			File:    true,
		},
		Tools: ToolsConfig{
			MaxFileSize:        1 << 20, // 1 MiB
			MaxMatches:         100,
			MaxLineLen:         500,
			MaxGlobItems:       1000,
			MaxWriteSize:       1 << 20,  // 1 MiB
			MaxMultiWriteBytes: 10 << 20, // 10 MiB
			MaxMultiWriteFiles: 50,
			MaxReplaceEdits:    50,

			HTTPMaxBody:      5 << 20, // 5 MiB
			HTTPTimeoutMs:    600000,
			HTTPBlockPrivate: true,

			// ShellBlockRegexes defaults to empty = no blacklist restrictions
			ShellMaxOutput: 256 << 10,

			WebSearchTimeoutMs: 600000,
		},
		Providers: []LLMProvider{
			{
				ID:        "deepseek",
				Name:      "DeepSeek",
				BaseURL:   "https://api.deepseek.com/v1",
				APIKeyEnv: "DEEPSEEK_API_KEY",
				Enabled:   true,
				IsDefault: true,
				TimeoutMs: 0,
				Retry: RetryConfig{
					MaxRetries:        3,
					InitialDelayMs:    1000,
					MaxDelayMs:        30000,
					BackoffMultiplier: 2.0,
				},
			},
		},
		Models: []LLMModel{
			{
				ID:            "deepseek-v4-flash",
				ProviderID:    "deepseek",
				Name:          "DeepSeek V4 Flash",
				ContextWindow: 1048576,
				Enabled:       true,
				Generation: GenerationParams{
					Temperature: 0,
					MaxTokens:   16384,
				},
			},
			{
				ID:            "deepseek-v4-flash-thinking",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-flash",
				Name:          "DeepSeek V4 Flash (Thinking)",
				ContextWindow: 1048576,
				Enabled:       true,
				Generation: GenerationParams{
					Temperature: 0,
					MaxTokens:   16384,
				},
				Thinking: ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "high",
				},
			},
			{
				ID:            "deepseek-v4-flash-thinking-max",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-flash",
				Name:          "DeepSeek V4 Flash (Max Reasoning)",
				ContextWindow: 1048576,
				Enabled:       true,
				Generation: GenerationParams{
					Temperature: 0,
					MaxTokens:   16384,
				},
				Thinking: ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "max",
				},
			},
			{
				ID:            "deepseek-v4-pro",
				ProviderID:    "deepseek",
				Name:          "DeepSeek V4 Pro",
				ContextWindow: 1048576,
				Enabled:       true,
				Generation: GenerationParams{
					Temperature: 0,
					MaxTokens:   16384,
				},
				Thinking: ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "high",
				},
			},
			{
				ID:            "deepseek-v4-pro-max",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-pro",
				Name:          "DeepSeek V4 Pro (Max Reasoning)",
				ContextWindow: 1048576,
				Enabled:       true,
				Generation: GenerationParams{
					Temperature: 0,
					MaxTokens:   16384,
				},
				Thinking: ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "max",
				},
			},
		},
		Embedding: EmbeddingConfig{
			Enabled:       false,
			MinSimilarity: 0.65,
			Providers: []EmbeddingProvider{
				{
					ID:        "local",
					Name:      "Local (Ollama)",
					BaseURL:   "http://localhost:11434",
					APIKeyEnv: "",
					Enabled:   false,
				},
			},
			Models: []EmbeddingModel{
				{
					ID:         "nomic-embed-text:latest",
					ProviderID: "local",
					Name:       "nomic-embed-text:latest",
					Dimension:  1024,
					BatchSize:  32,
					Normalize:  true,
					Enabled:    false,
					IsDefault:  true,
				},
			},
		},
		ModelRoutes: ModelRoutesConfig{
			General:     "deepseek:deepseek-v4-flash-thinking",
			Engineering: "deepseek:deepseek-v4-flash-thinking-max",
			Research:    "deepseek:deepseek-v4-flash-thinking-max",
			Classifier:  "deepseek:deepseek-v4-flash",
			Vision:      "",
			Fallback:    "deepseek:deepseek-v4-flash",
		},
		Agent: AgentConfig{},
		Simulation: SimulationConfig{
			DefaultMaxWallClockMs: 1080000,
			EnableReflection:      true,
			SimulatedHours:        168,
			TickIntervalMs:        1000,
			TimeScale:             300,
			Language:              "zh",
		},
		Speech: SpeechConfig{
			Enabled: false,
			Model:   "small",
		},
	}
}
