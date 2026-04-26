package config

// DefaultSettings 返回系统硬编码的默认配置（最低优先级）
// 对应 TypeScript 端 seeds.ts 中的 DEFAULT_CONFIGS 和 DEFAULT_LLM_CONFIGS
func DefaultSettings() Settings {
	return Settings{
		Session: SessionConfig{
			TimeoutSecs: 3600,
			MaxHistory:  1000,
			AutoSave:    true,
		},
		Log: LogConfig{
			Level:         "info",
			Console:       true,
			File:          true,
			MaxDays:       30,
			MaxFileSizeMB: 50,
		},
		Tools: ToolsConfig{
			// AllowedDirs 默认空：serveCmd 启动时注入 workDir/cwd
			MaxFileSize:        1 << 20,  // 1 MiB
			MaxMatches:         100,
			MaxLineLen:         500,
			MaxGlobItems:       1000,
			MaxWriteSize:       1 << 20,  // 1 MiB
			MaxMultiWriteBytes: 10 << 20, // 10 MiB
			MaxMultiWriteFiles: 50,
			MaxReplaceEdits:    50,

			HTTPMaxBody:      5 << 20, // 5 MiB
			HTTPTimeoutMs:    10000,
			HTTPBlockPrivate: true,

			// ShellBlockRegexes 默认空 = 无黑名单限制
			// ShellConfirmRegexes 默认包含常见危险命令
			ShellConfirmRegexes: []string{
				`^\s*rm\b`,
				`^\s*dd\b`,
				`^\s*mkfs\b`,
				`^\s*bash\b`,
				`^\s*sh\b`,
				`^\s*format\b`,
				`^\s*diskpart\b`,
			},
			ShellTimeoutMs: 30000,
			ShellMaxOutput: 256 << 10,

			TavilyAPIKeyEnv: "TAVILY_API_KEY",
			TavilyEndpoint:  "https://api.tavily.com/search",
			TavilyTimeoutMs: 15000,
		},
		Providers: []LLMProvider{
			{
				ID:        "deepseek",
				Name:      "DeepSeek",
				BaseURL:   "https://api.deepseek.com/v1",
				APIKeyEnv: "DEEPSEEK_API_KEY",
				Enabled:   true,
				IsDefault: true,
				Capabilities: []string{
					"chat",
					"streaming",
					"function-calling",
				},
				TimeoutMs: 120000,
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
				Type:          "chat",
				ContextWindow: 1048576,
				Enabled:       true,
				IsDefault:     true,
				Generation: GenerationParams{
					Temperature:      0,
					MaxTokens:        4096,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					StopSequences:    []string{},
				},
				Thinking: ThinkingConfig{
					Enabled:         false,
					BudgetTokens:    0,
					Type:            "",
					ReasoningEffort: "",
				},
				Capabilities: ModelCapability{
					Streaming:       true,
					FunctionCalling: true,
					Vision:          false,
					ImageInput:      false,
					Thinking:        true,
					JSONMode:        false,
				},
			},
			{
				ID:            "deepseek-v4-flash-thinking",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-flash",
				Name:          "DeepSeek V4 Flash (Thinking)",
				Type:          "chat",
				ContextWindow: 1048576,
				Enabled:       true,
				IsDefault:     false,
				Generation: GenerationParams{
					Temperature:      0,
					MaxTokens:        8192,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					StopSequences:    []string{},
				},
				Thinking: ThinkingConfig{
					Enabled:         true,
					BudgetTokens:    0,
					Type:            "reasoning",
					ReasoningEffort: "high",
				},
				Capabilities: ModelCapability{
					Streaming:       true,
					FunctionCalling: true,
					Vision:          false,
					ImageInput:      false,
					Thinking:        true,
					JSONMode:        false,
				},
			},
			{
				ID:            "deepseek-v4-pro",
				ProviderID:    "deepseek",
				Name:          "DeepSeek V4 Pro",
				Type:          "chat",
				ContextWindow: 1048576,
				Enabled:       true,
				IsDefault:     false,
				Generation: GenerationParams{
					Temperature:      0,
					MaxTokens:        8192,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					StopSequences:    []string{},
				},
				Thinking: ThinkingConfig{
					Enabled:         true,
					BudgetTokens:    0,
					Type:            "reasoning",
					ReasoningEffort: "high",
				},
				Capabilities: ModelCapability{
					Streaming:       true,
					FunctionCalling: true,
					Vision:          false,
					ImageInput:      false,
					Thinking:        true,
					JSONMode:        false,
				},
			},
		},
		Embedding: EmbeddingConfig{
			Enabled: false,
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
					ID:         "bge-large-zh-v1.5",
					ProviderID: "local",
					Name:       "BGE Large ZH v1.5",
					Dimension:  1024,
					BatchSize:  32,
					Normalize:  true,
					Enabled:    false,
					IsDefault:  true,
				},
			},
		},
	}
}
