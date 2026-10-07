package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func scanGroup(row interface{ Scan(...any) error }) (domain.A2AGroup, error) {
	var g domain.A2AGroup
	err := row.Scan(&g.ID, &g.Name, &g.Title, &g.Description, &g.InviteHash)
	return g, err
}

func (s *Store) ListA2AGroups() ([]domain.A2AGroup, error) {
	rows, err := s.DB.Query(`SELECT id, name, title, description, invite_hash FROM a2a_groups ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.A2AGroup
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) GetA2AGroup(id int64) (domain.A2AGroup, error) {
	return scanGroup(s.DB.QueryRow(`SELECT id, name, title, description, invite_hash FROM a2a_groups WHERE id=?`, id))
}

func (s *Store) GetA2AGroupByInvite(hash string) (domain.A2AGroup, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return domain.A2AGroup{}, sql.ErrNoRows
	}
	return scanGroup(s.DB.QueryRow(`SELECT id, name, title, description, invite_hash FROM a2a_groups WHERE invite_hash=?`, hash))
}

func (s *Store) SaveA2AGroup(g domain.A2AGroup) (int64, error) {
	name, err := domain.NormalizeMCPName(g.Name)
	if err != nil {
		return 0, errGroupName
	}
	title := clipStore(g.Title, 80)
	if title == "" {
		title = name
	}
	desc := clipStore(g.Description, 500)
	if g.ID == 0 {
		res, err := s.DB.Exec(`INSERT INTO a2a_groups (name, title, description, invite_hash) VALUES (?,?,?,?)`,
			name, title, desc, strings.TrimSpace(g.InviteHash))
		if err != nil {
			return 0, mapGroupErr(err)
		}
		return res.LastInsertId()
	}
	res, err := s.DB.Exec(`UPDATE a2a_groups SET name=?, title=?, description=? WHERE id=?`, name, title, desc, g.ID)
	if err != nil {
		return 0, mapGroupErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, sql.ErrNoRows
	}
	return g.ID, nil
}

func (s *Store) SetA2AInvite(id int64, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return errGroupName
	}
	res, err := s.DB.Exec(`UPDATE a2a_groups SET invite_hash=? WHERE id=?`, hash, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteA2AGroup(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM a2a_groups WHERE id=?`, id)
	return err
}

func (s *Store) AgentGroupIDs(agentID int64) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT group_id FROM a2a_group_members WHERE agent_id=? ORDER BY group_id`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) AgentGroups(agentID int64) ([]domain.A2AGroup, error) {
	rows, err := s.DB.Query(`SELECT g.id, g.name, g.title, g.description, g.invite_hash
FROM a2a_groups g JOIN a2a_group_members m ON m.group_id=g.id
WHERE m.agent_id=? ORDER BY g.name, g.id`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.A2AGroup
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) SetAgentGroups(agentID int64, groupIDs []int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM a2a_group_members WHERE agent_id=?`, agentID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, id := range groupIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.Exec(`INSERT INTO a2a_group_members (group_id, agent_id) VALUES (?,?)`, id, agentID); err != nil {
			return mapGroupErr(err)
		}
	}
	return tx.Commit()
}

func (s *Store) AddAgentToGroup(agentID, groupID int64) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO a2a_group_members (group_id, agent_id) VALUES (?,?)`, groupID, agentID)
	return err
}

func (s *Store) ListGroupAgents(groupID int64) ([]domain.A2AUpstream, error) {
	rows, err := s.DB.Query(`SELECT `+a2aCols+` FROM a2a_upstreams u
JOIN a2a_group_members m ON m.agent_id=u.id
WHERE m.group_id=? ORDER BY u.name, u.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.A2AUpstream
	for rows.Next() {
		u, err := scanA2A(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) InsertA2AMessage(m domain.A2AMessage) (domain.A2AMessage, error) {
	m.FromAgent = strings.TrimSpace(m.FromAgent)
	m.ToAgent = strings.TrimSpace(m.ToAgent)
	m.Body = clipStore(m.Body, 8000)
	if m.FromAgent == "" || m.Body == "" {
		return domain.A2AMessage{}, errMessage
	}
	if m.Status == "" {
		m.Status = domain.A2APosted
	}
	if m.Status != domain.A2APosted && m.Status != domain.A2APending && m.Status != domain.A2ARejected {
		return domain.A2AMessage{}, errMessage
	}
	m.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	res, err := s.DB.Exec(`INSERT INTO a2a_messages (group_id, from_agent, to_agent, body, status, created_at) VALUES (?,?,?,?,?,?)`,
		m.GroupID, m.FromAgent, m.ToAgent, m.Body, m.Status, m.CreatedAt)
	if err != nil {
		return domain.A2AMessage{}, err
	}
	m.ID, _ = res.LastInsertId()
	return m, nil
}

func (s *Store) ListA2AMessages(groupID, after int64, limit int) ([]domain.A2AMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if after < 0 {
		after = 0
	}
	rows, err := s.DB.Query(`SELECT id, group_id, from_agent, to_agent, body, status, created_at
FROM a2a_messages WHERE group_id=? AND id>? ORDER BY id LIMIT ?`, groupID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *Store) ListA2ATranscript(groupID int64, limit int) ([]domain.A2AMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	rows, err := s.DB.Query(`SELECT id, group_id, from_agent, to_agent, body, status, created_at FROM (
SELECT id, group_id, from_agent, to_agent, body, status, created_at
FROM a2a_messages WHERE group_id=? ORDER BY id DESC LIMIT ?
) ORDER BY id`, groupID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]domain.A2AMessage, error) {
	var out []domain.A2AMessage
	for rows.Next() {
		var m domain.A2AMessage
		if err := rows.Scan(&m.ID, &m.GroupID, &m.FromAgent, &m.ToAgent, &m.Body, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetA2AMessage(id int64) (domain.A2AMessage, error) {
	var m domain.A2AMessage
	err := s.DB.QueryRow(`SELECT id, group_id, from_agent, to_agent, body, status, created_at FROM a2a_messages WHERE id=?`, id).
		Scan(&m.ID, &m.GroupID, &m.FromAgent, &m.ToAgent, &m.Body, &m.Status, &m.CreatedAt)
	return m, err
}

func (s *Store) SetA2AMessageStatus(id int64, status string) error {
	if status != domain.A2APosted && status != domain.A2ARejected {
		return errMessage
	}
	res, err := s.DB.Exec(`UPDATE a2a_messages SET status=? WHERE id=? AND status=?`, status, id, domain.A2APending)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func clipStore(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

type groupNameError struct{}

func (groupNameError) Error() string { return "group name" }

var errGroupName error = groupNameError{}

type groupNameTaken struct{}

func (groupNameTaken) Error() string { return "group name taken" }

var errGroupTaken error = groupNameTaken{}

type messageError struct{}

func (messageError) Error() string { return "message" }

var errMessage error = messageError{}

func mapGroupErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE") {
		return errGroupTaken
	}
	if strings.Contains(msg, "FOREIGN KEY") {
		return errGroupName
	}
	return err
}
