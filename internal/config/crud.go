package config

import "fmt"

// CreateProvider adds a new LLM provider to the configuration.
func (s *GlobalService) CreateProvider(p LLMProvider) error {
	if p.ID == "" {
		return fmt.Errorf("provider id is required")
	}
	settings := s.Get()
	for _, existing := range settings.Providers {
		if existing.ID == p.ID {
			return fmt.Errorf("provider %q already exists", p.ID)
		}
	}
	_, err := s.Set(func(st *Settings) {
		if p.IsDefault {
			for i := range st.Providers {
				st.Providers[i].IsDefault = false
			}
		}
		st.Providers = append(st.Providers, p)
	})
	return err
}

// UpdateProvider updates an existing LLM provider.
func (s *GlobalService) UpdateProvider(id string, p LLMProvider) error {
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	var found bool
	_, err := s.Set(func(st *Settings) {
		for i := range st.Providers {
			if st.Providers[i].ID == id {
				p.ID = id
				if p.IsDefault {
					for j := range st.Providers {
						st.Providers[j].IsDefault = false
					}
				}
				st.Providers[i] = p
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("provider %q not found", id)
	}
	return nil
}

// DeleteProvider removes an LLM provider by ID.
func (s *GlobalService) DeleteProvider(id string) error {
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	var deleted bool
	_, err := s.Set(func(st *Settings) {
		var providers []LLMProvider
		for _, p := range st.Providers {
			if p.ID != id {
				providers = append(providers, p)
			} else {
				deleted = true
			}
		}
		st.Providers = providers
	})
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("provider %q not found", id)
	}
	return nil
}

// CreateModel adds a new LLM model to the configuration.
func (s *GlobalService) CreateModel(m LLMModel) error {
	if m.ID == "" {
		return fmt.Errorf("model id is required")
	}
	if m.ProviderID == "" {
		return fmt.Errorf("model provider id is required")
	}
	settings := s.Get()
	for _, existing := range settings.Models {
		if existing.ProviderID == m.ProviderID && existing.ID == m.ID {
			return fmt.Errorf("model %q already exists under provider %q", m.ID, m.ProviderID)
		}
	}
	_, err := s.Set(func(st *Settings) {
		st.Models = append(st.Models, m)
	})
	return err
}

// UpdateModel updates an existing LLM model identified by (providerID, id).
func (s *GlobalService) UpdateModel(providerID, id string, m LLMModel) error {
	if id == "" || providerID == "" {
		return fmt.Errorf("model id and provider id are required")
	}
	var found bool
	_, err := s.Set(func(st *Settings) {
		for i := range st.Models {
			if st.Models[i].ProviderID == providerID && st.Models[i].ID == id {
				m.ProviderID = providerID
				m.ID = id
				st.Models[i] = m
				found = true
				return
			}
		}
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("model %q under provider %q not found", id, providerID)
	}
	return nil
}

// DeleteModel removes an LLM model identified by (providerID, id).
func (s *GlobalService) DeleteModel(providerID, id string) error {
	if id == "" || providerID == "" {
		return fmt.Errorf("model id and provider id are required")
	}
	var deleted bool
	_, err := s.Set(func(st *Settings) {
		var models []LLMModel
		for _, m := range st.Models {
			if m.ProviderID == providerID && m.ID == id {
				deleted = true
			} else {
				models = append(models, m)
			}
		}
		st.Models = models
	})
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("model %q under provider %q not found", id, providerID)
	}
	return nil
}

// UpdateModelRoutes replaces the task model routing configuration.
func (s *GlobalService) UpdateModelRoutes(routes ModelRoutesConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.ModelRoutes = routes
	})
	return err
}

// UpdateTools replaces the tools configuration.
func (s *GlobalService) UpdateTools(tools ToolsConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.Tools = tools
	})
	return err
}

// UpdateQQBots replaces the QQ bot configuration.
func (s *GlobalService) UpdateQQBots(bots []QQBotConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.QQBots = bots
	})
	return err
}

// UpdateWechatBots replaces the WeChat bot configuration.
func (s *GlobalService) UpdateWechatBots(bots []WechatBotConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.WechatBots = bots
	})
	return err
}

// UpdateTelegramBots replaces Telegram bot configuration.
func (s *GlobalService) UpdateTelegramBots(bots []TelegramBotConfig) error {
	_, err := s.Set(func(st *Settings) { st.TelegramBots = bots })
	return err
}

// UpdateLSPMCP replaces the LSP MCP configuration.
func (s *GlobalService) UpdateLSPMCP(lspmcp LSPMCPConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.LSPMCP = lspmcp
	})
	return err
}

// UpdateEmbedding replaces the embedding configuration.
func NormalizeEmbedding(emb EmbeddingConfig) EmbeddingConfig {
	if emb.Local.Model == "" {
		emb.Local.Model = "multilingual-e5-small"
	}
	// Device selection is automatic in product configuration; the field is
	// retained for compatibility with previously saved JSON/YAML.
	emb.Local.Device = "auto"
	return emb
}
func ValidateEmbedding(emb EmbeddingConfig) error {
	if emb.Local.Model == "" {
		emb.Local.Model = "multilingual-e5-small"
	}
	if emb.Local.Device == "" {
		emb.Local.Device = "auto"
	}
	if emb.Provider == "onnx" {
		if emb.Local.Model != "multilingual-e5-small" {
			return fmt.Errorf("unsupported local embedding model: %s", emb.Local.Model)
		}
		if emb.Local.Device != "auto" && emb.Local.Device != "cpu" {
			return fmt.Errorf("unsupported local embedding device: %s", emb.Local.Device)
		}
	}
	return nil
}
func (s *GlobalService) UpdateEmbedding(emb EmbeddingConfig) error {
	if err := ValidateEmbedding(emb); err != nil {
		return err
	}
	emb = NormalizeEmbedding(emb)
	_, err := s.Set(func(st *Settings) {
		st.Embedding = emb
	})
	return err
}

// UpdateSession replaces the session configuration.
func (s *GlobalService) UpdateSession(sess SessionConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.Session = sess
	})
	return err
}

// UpdateSpeech replaces the speech-to-text configuration.
func (s *GlobalService) UpdateSpeech(speech SpeechConfig) error {
	_, err := s.Set(func(st *Settings) {
		st.Speech = speech
	})
	return err
}
