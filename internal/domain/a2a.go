package domain

// A2AUpstream is an Agent2Agent endpoint this gateway proxies.
// The token never leaves the node. Hub share publishes the name only.
type A2AUpstream struct {
	ID       int64
	Name     string
	URL      string
	Token    string
	Enabled  bool
	HubShare bool
}
