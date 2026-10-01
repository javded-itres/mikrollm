package store

import (
	"database/sql"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func scanA2A(row interface{ Scan(...any) error }) (domain.A2AUpstream, error) {
	var u domain.A2AUpstream
	var en, share int
	err := row.Scan(&u.ID, &u.Name, &u.URL, &u.Token, &en, &share)
	u.Enabled = en == 1
	u.HubShare = share == 1
	return u, err
}

func (s *Store) ListA2A() ([]domain.A2AUpstream, error) {
	rows, err := s.DB.Query(`SELECT id, name, url, token, enabled, hub_share FROM a2a_upstreams ORDER BY name, id`)
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
	rows, err := s.DB.Query(`SELECT id, name, url, token, enabled, hub_share FROM a2a_upstreams WHERE enabled=1 AND hub_share=1 ORDER BY name, id`)
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
	return scanA2A(s.DB.QueryRow(`SELECT id, name, url, token, enabled, hub_share FROM a2a_upstreams WHERE id=?`, id))
}

func (s *Store) GetA2AByName(name string) (domain.A2AUpstream, error) {
	name, err := domain.NormalizeMCPName(name)
	if err != nil {
		return domain.A2AUpstream{}, errAgentName
	}
	return scanA2A(s.DB.QueryRow(`SELECT id, name, url, token, enabled, hub_share FROM a2a_upstreams WHERE name=?`, name))
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
	if !domain.ValidMCPURL(rawURL) {
		return 0, errAgentURL
	}
	if u.ID > 0 && u.Token == "" {
		old, err := s.GetA2A(u.ID)
		if err != nil {
			return 0, err
		}
		u.Token = old.Token
	}
	en, share := 0, 0
	if u.Enabled {
		en = 1
	}
	if u.HubShare {
		share = 1
	}
	if u.ID == 0 {
		res, err := s.DB.Exec(`INSERT INTO a2a_upstreams (name, url, token, enabled, hub_share) VALUES (?,?,?,?,?)`,
			name, rawURL, u.Token, en, share)
		if err != nil {
			return 0, mapAgentErr(err)
		}
		return res.LastInsertId()
	}
	res, err := s.DB.Exec(`UPDATE a2a_upstreams SET name=?, url=?, token=?, enabled=?, hub_share=? WHERE id=?`,
		name, rawURL, u.Token, en, share, u.ID)
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
