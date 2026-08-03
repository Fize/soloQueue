package mcp

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

const (
	PolicyNeedsReview = "needs_review"
	PolicyApproved    = "approved"
	PolicyRevoked     = "revoked"
)

// Policy is stored separately from the protocol-compatible mcp.json
// definition. Approval is bound to the exact definition digest and runtime.
type Policy struct {
	Scope            string            `json:"scope"`
	ServerName       string            `json:"server_name"`
	Runtime          tools.RuntimeType `json:"runtime"`
	NetworkEnabled   bool              `json:"network_enabled"`
	State            string            `json:"state"`
	Revision         int64             `json:"revision"`
	DefinitionDigest string            `json:"definition_digest"`
	ApprovedAt       string            `json:"approved_at,omitempty"`
	UpdatedAt        string            `json:"updated_at,omitempty"`
}

type PolicyStore struct {
	db *sqlitedb.DB
}

func NewPolicyStore(db *sqlitedb.DB) *PolicyStore {
	if db == nil {
		return nil
	}
	return &PolicyStore{db: db}
}

func DefinitionDigest(cfg ServerConfig) string {
	data, _ := json.Marshal(struct {
		Name      string            `json:"name"`
		Command   string            `json:"command"`
		Args      []string          `json:"args"`
		Env       map[string]string `json:"env"`
		Transport string            `json:"transport"`
		Enabled   bool              `json:"enabled"`
	}{
		Name: cfg.Name, Command: cfg.Command, Args: cfg.Args, Env: cfg.Env,
		Transport: cfg.Transport, Enabled: cfg.Enabled,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func NormalizePolicyScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "global"
	}
	if strings.HasPrefix(scope, "project:") {
		path := strings.TrimSpace(strings.TrimPrefix(scope, "project:"))
		if path != "" {
			return "project:" + filepath.Clean(path)
		}
	}
	return scope
}

// Effective returns a fail-closed policy. Missing or stale approvals become
// needs_review and default to Sandbox, without mutating policy history.
func (s *PolicyStore) Effective(ctx context.Context, scope string, cfg ServerConfig) (Policy, error) {
	scope = NormalizePolicyScope(scope)
	digest := DefinitionDigest(cfg)
	fallback := Policy{
		Scope: scope, ServerName: cfg.Name, Runtime: tools.RuntimeSandbox,
		State: PolicyNeedsReview, DefinitionDigest: digest,
	}
	if s == nil || s.db == nil {
		return fallback, nil
	}

	var policy Policy
	err := s.db.QueryRowContext(ctx, `
		SELECT scope, server_name, runtime, network_enabled, state, revision, definition_digest,
		       approved_at, updated_at
		FROM mcp_policies
		WHERE scope = ? AND server_name = ?
	`, scope, cfg.Name).Scan(
		&policy.Scope, &policy.ServerName, &policy.Runtime, &policy.NetworkEnabled, &policy.State,
		&policy.Revision, &policy.DefinitionDigest, &policy.ApprovedAt, &policy.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return fallback, nil
		}
		return Policy{}, err
	}
	if policy.Runtime != tools.RuntimeHost && policy.Runtime != tools.RuntimeSandbox {
		policy.Runtime = tools.RuntimeSandbox
		policy.State = PolicyNeedsReview
	}
	if policy.DefinitionDigest != digest {
		policy.State = PolicyNeedsReview
		policy.DefinitionDigest = digest
		policy.ApprovedAt = ""
	}
	return policy, nil
}

func (s *PolicyStore) Approve(
	ctx context.Context,
	scope string,
	cfg ServerConfig,
	runtime tools.RuntimeType,
	network ...bool,
) (Policy, error) {
	if s == nil || s.db == nil {
		return Policy{}, fmt.Errorf("MCP policy store unavailable")
	}
	if runtime != tools.RuntimeHost && runtime != tools.RuntimeSandbox {
		return Policy{}, fmt.Errorf("invalid MCP runtime %q", runtime)
	}
	scope = NormalizePolicyScope(scope)
	digest := DefinitionDigest(cfg)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	networkEnabled := len(network) > 0 && network[0]

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_policies (
			scope, server_name, runtime, network_enabled, state, revision, definition_digest,
			approved_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT(scope, server_name) DO UPDATE SET
			runtime = excluded.runtime,
			network_enabled = excluded.network_enabled,
			state = excluded.state,
			revision = mcp_policies.revision + 1,
			definition_digest = excluded.definition_digest,
			approved_at = excluded.approved_at,
			updated_at = excluded.updated_at
	`, scope, cfg.Name, runtime, networkEnabled, PolicyApproved, digest, now, now)
	if err != nil {
		return Policy{}, err
	}
	return s.Effective(ctx, scope, cfg)
}

func (s *PolicyStore) Revoke(ctx context.Context, scope, serverName string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("MCP policy store unavailable")
	}
	scope = NormalizePolicyScope(scope)
	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()
	_, err := s.db.ExecContext(ctx, `
		UPDATE mcp_policies
		SET state = ?, revision = revision + 1, approved_at = '',
		    updated_at = ?
		WHERE scope = ? AND server_name = ?
	`, PolicyRevoked, time.Now().UTC().Format(time.RFC3339Nano), scope, serverName)
	return err
}

func (s *PolicyStore) CountApprovedHost(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM mcp_policies
		WHERE state = ? AND runtime = ?
	`, PolicyApproved, tools.RuntimeHost).Scan(&count)
	return count, err
}
