package server

import (
	"fmt"
	"strings"
)

func (m *Mux) normalizeChannelBinding(bindType, bindAgent string) (string, string, error) {
	bindType = strings.TrimSpace(bindType)
	if bindType == "" {
		bindType = "l1"
	}
	if bindType != "l1" && bindType != "l2" {
		return "", "", fmt.Errorf("invalid bind_type %q", bindType)
	}
	bindAgent = strings.TrimSpace(bindAgent)
	if bindType == "l1" {
		return bindType, "", nil
	}
	if bindAgent == "" {
		return "", "", fmt.Errorf("bind_agent is required for l2 binding")
	}
	return bindType, bindAgent, nil
}
