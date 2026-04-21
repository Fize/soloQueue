package config

// DefaultSettings 返回系统硬编码的默认配置（最低优先级）
// 对应 TypeScript 端 seeds.ts 中的 DEFAULT_CONFIGS 和 DEFAULT_LLM_CONFIGS
func DefaultSettings() Settings {
	return Settings{
		App: AppConfig{
			Theme:    "dark",
			Language: "zh-CN",
		},
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
				ID:            "deepseek-chat",
				ProviderID:    "deepseek",
				Name:          "DeepSeek Chat",
				Type:          "chat",
				ContextWindow: 64000,
				Enabled:       true,
				IsDefault:     true,
				Generation: GenerationParams{
					Temperature:      0.7,
					MaxTokens:        4096,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					StopSequences:    []string{},
				},
				Thinking: ThinkingConfig{
					Enabled:      false,
					BudgetTokens: 0,
					Type:         "",
				},
				Capabilities: ModelCapability{
					Streaming:       true,
					FunctionCalling: true,
					Vision:          false,
					ImageInput:      false,
					Thinking:        false,
					JSONMode:        false,
				},
			},
			{
				ID:            "deepseek-coder",
				ProviderID:    "deepseek",
				Name:          "DeepSeek Coder",
				Type:          "code",
				ContextWindow: 128000,
				Enabled:       true,
				IsDefault:     false,
				Generation: GenerationParams{
					Temperature:      0.2,
					MaxTokens:        8192,
					TopP:             1.0,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					StopSequences:    []string{},
				},
				Thinking: ThinkingConfig{
					Enabled:      false,
					BudgetTokens: 0,
					Type:         "",
				},
				Capabilities: ModelCapability{
					Streaming:       true,
					FunctionCalling: true,
					Vision:          false,
					ImageInput:      false,
					Thinking:        false,
					JSONMode:        false,
				},
			},
			{
				ID:            "deepseek-reasoner",
				ProviderID:    "deepseek",
				Name:          "DeepSeek Reasoner",
				Type:          "chat",
				ContextWindow: 64000,
				Enabled:       true,
				IsDefault:     false,
				Generation: GenerationParams{
					Temperature:      0.6,
					MaxTokens:        8192,
					TopP:             0.95,
					FrequencyPenalty: 0.0,
					PresencePenalty:  0.0,
					StopSequences:    []string{},
				},
				Thinking: ThinkingConfig{
					Enabled:      true,
					BudgetTokens: 4096,
					Type:         "reasoning",
				},
				Capabilities: ModelCapability{
					Streaming:       true,
					FunctionCalling: false,
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
