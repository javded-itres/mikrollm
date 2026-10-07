package store

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

const a2aCols = `id, name, url, token, enabled, hub_share, display_name, company, description, skills, auth_hash`

func scanA2A(row interface{ Scan(...any) error }) (domain.A2AUpstream, error) {
	var u domain.A2AUpstream
	var en, share int
	var skills string
	err := row.Scan(&u.ID, &u.Name, &u.URL, &u.Token, &en, &share, &u.DisplayName, &u.Company, &u.Description, &skills, &u.AuthHash)
	u.Enabled = en == 1
	u.HubShare = share == 1
	if skills != "" && skills != "[]" {
		_ = json.Unmarshal([]byte(skills), &u.Skills)
	}
	return u, err
}

func skillsJSON(skills []domain.A2ASkill) string {
	if skills == nil {
		skills = []domain.A2ASkill{}
	}
	raw, err := json.Marshal(skills)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func (s *Store) ListA2A() ([]domain.A2AUpstream, error) {
	rows, err := s.DB.Query(`SELECT ` + a2aCols + ` FROM a2a_upstreams ORDER BY name, id`)
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

func (s *Store) ListSharedA2A() ([]domain.A2AUpstream, error) {
	rows, err := s.DB.Query(`SELECT ` + a2aCols + ` FROM a2a_upstreams WHERE enabled=1 AND hub_share=1 ORDER BY name, id`)
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

func (s *Store) GetA2A(id int64) (domain.A2AUpstream, error) {
	return scanA2A(s.DB.QueryRow(`SELECT `+a2aCols+` FROM a2a_upstreams WHERE id=?`, id))
}

func (s *Store) GetA2AByName(name string) (domain.A2AUpstream, error) {
	name, err := domain.NormalizeMCPName(name)
	if err != nil {
		return domain.A2AUpstream{}, errAgentName
	}
	return scanA2A(s.DB.QueryRow(`SELECT `+a2aCols+` FROM a2a_upstreams WHERE name=?`, name))
}

func (s *Store) GetA2AByAuthHash(hash string) (domain.A2AUpstream, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return domain.A2AUpstream{}, sql.ErrNoRows
	}
	return scanA2A(s.DB.QueryRow(`SELECT `+a2aCols+` FROM a2a_upstreams WHERE auth_hash=?`, hash))
}

func (s *Store) A2AUpstream(name string) (domain.A2AUpstream, error) {
	return s.GetA2AByName(name)
}

func (s *Store) SaveA2A(u domain.A2AUpstream) (int64, error) {
	name, err := domain.NormalizeMCPName(u.Name)
	if err != nil {
		return 0, errAgentName
	}
	rawURL := strings.TrimSpace(u.URL)
	if rawURL != "" && !domain.ValidMCPURL(rawURL) {
		return 0, errAgentURL
	}
	u.Clean()
	if u.ID > 0 && (u.Token == "" || u.AuthHash == "") {
		old, err := s.GetA2A(u.ID)
		if err != nil {
			return 0, err
		}
		if u.Token == "" {
			u.Token = old.Token
		}
		if u.AuthHash == "" {
			u.AuthHash = old.AuthHash
		}
	}
	en, share := 0, 0
	if u.Enabled {
		en = 1
	}
	if u.HubShare {
		share = 1
	}
	skills := skillsJSON(u.Skills)
	if u.ID == 0 {
		res, err := s.DB.Exec(`INSERT INTO a2a_upstreams (name, url, token, enabled, hub_share, display_name, company, description, skills, auth_hash) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			name, rawURL, u.Token, en, share, u.DisplayName, u.Company, u.Description, skills, u.AuthHash)
		if err != nil {
			return 0, mapAgentErr(err)
		}
		return res.LastInsertId()
	}
	res, err := s.DB.Exec(`UPDATE a2a_upstreams SET name=?, url=?, token=?, enabled=?, hub_share=?, display_name=?, company=?, description=?, skills=?, auth_hash=? WHERE id=?`,
		name, rawURL, u.Token, en, share, u.DisplayName, u.Company, u.Description, skills, u.AuthHash, u.ID)
	if err != nil {
		return 0, mapAgentErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, sql.ErrNoRows
	}
	return u.ID, nil
}

func (s *Store) DeleteA2A(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM a2a_upstreams WHERE id=?`, id)
	return err
}

type agentNameError struct{}

func (agentNameError) Error() string { return "agent name" }

var errAgentName error = agentNameError{}

type agentURLError struct{}

func (agentURLError) Error() string { return "agent url" }

var errAgentURL error = agentURLError{}

type agentNameTaken struct{}

func (agentNameTaken) Error() string { return "agent name taken" }

var errAgentTaken error = agentNameTaken{}

func mapAgentErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE") {
		return errAgentTaken
	}
	return err
}
