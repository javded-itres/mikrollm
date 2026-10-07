package domain

import "strings"

const (
	A2APosted   = "posted"
	A2APending  = "pending"
	A2ARejected = "rejected"
)

// A2ASkill is one published capability on an agent card.
type A2ASkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// A2AUpstream is an Agent2Agent endpoint this gateway proxies.
// Token is the upstream bearer and never leaves the node.
// AuthHash is the agent's own agt- bearer, stored as SHA-256.
// Hub share publishes the name only.
type A2AUpstream struct {
	ID          int64
	Name        string
	URL         string
	Token       string
	Enabled     bool
	HubShare    bool
	DisplayName string
	Company     string
	Description string
	Skills      []A2ASkill
	AuthHash    string
}

// A2AGroup is a visibility boundary. Members see only agents that share a group.
type A2AGroup struct {
	ID          int64
	Name        string
	Title       string
	Description string
	InviteHash  string
}

// A2AMessage is one line in a group transcript.
// Group is filled by the caller and is not a stored column.
type A2AMessage struct {
	ID        int64
	GroupID   int64
	Group     string
	FromAgent string
	ToAgent   string
	Body      string
	Status    string
	CreatedAt string
}

// Clean clamps the published profile. An empty URL is a poll-only agent.
func (u *A2AUpstream) Clean() {
	u.DisplayName = clipText(u.DisplayName, 80)
	u.Company = clipText(u.Company, 80)
	u.Description = clipText(u.Description, 2000)
	var skills []A2ASkill
	for _, sk := range u.Skills {
		id, err := NormalizeMCPName(sk.ID)
		if err != nil {
			id, err = NormalizeMCPName(slugSkill(sk.Name))
			if err != nil {
				continue
			}
		}
		name := clipText(sk.Name, 80)
		if name == "" {
			name = id
		}
		skills = append(skills, A2ASkill{ID: id, Name: name, Description: clipText(sk.Description, 400)})
		if len(skills) == 32 {
			break
		}
	}
	u.Skills = skills
}

// ParseSkillLines reads admin text: `id | name | description`, one skill per line.
func ParseSkillLines(raw string) []A2ASkill {
	var out []A2ASkill
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		var sk A2ASkill
		switch {
		case len(parts) == 1:
			sk.Name = parts[0]
			sk.ID = slugSkill(parts[0])
		case len(parts) == 2:
			sk.ID, sk.Name = parts[0], parts[1]
		default:
			sk.ID, sk.Name = parts[0], parts[1]
			sk.Description = strings.Join(parts[2:], " | ")
		}
		out = append(out, sk)
	}
	return out
}

// FormatSkillLines is the admin textarea form of Skills.
func FormatSkillLines(skills []A2ASkill) string {
	var b strings.Builder
	for i, sk := range skills {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(sk.ID)
		b.WriteString(" | ")
		b.WriteString(sk.Name)
		b.WriteString(" | ")
		b.WriteString(sk.Description)
	}
	return b.String()
}

func slugSkill(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == '_' || r == '-' || r == ' ':
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
