package domain

import (
	"net/url"
	"regexp"
	"strings"
)

// MCPUpstream is an MCP server this gateway proxies for agents.
// The token never leaves the node. Hub share publishes the name only.
type MCPUpstream struct {
	ID       int64
	Name     string
	URL      string
	Token    string
	Enabled  bool
	HubShare bool
}

var mcpNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// NormalizeMCPName accepts a short slug used in /mcp/u/{name} and in the hub catalog.
func NormalizeMCPName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !mcpNameRE.MatchString(name) {
		return "", errMCPName
	}
	return name, nil
}

// ValidMCPURL is an http(s) URL with a host and no userinfo. The token is stored separately.
func ValidMCPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

type mcpNameError struct{}

func (mcpNameError) Error() string { return "mcp name" }

var errMCPName error = mcpNameError{}
