package store

import (
	"fmt"
	"strings"

	"github.com/javded-itres/mikrollm/internal/domain"
)

func (s *Store) ListPolicies() ([]domain.Policy, error) {
	rows, err := s.DB.Query(`SELECT id, name, kind, action, mode, enabled, config FROM security_policies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []domain.Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tmap, err := s.allTargets()
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Targets = tmap[out[i].ID]
	}
	return out, nil
}

func (s *Store) GetPolicy(id int64) (domain.Policy, error) {
	row := s.DB.QueryRow(`SELECT id, name, kind, action, mode, enabled, config FROM security_policies WHERE id=?`, id)
	p, err := scanPolicy(row)
	if err != nil {
		return p, err
	}
	tmap, err := s.allTargets()
	if err != nil {
		return p, err
	}
	p.Targets = tmap[p.ID]
	return p, nil
}

func (s *Store) SavePolicy(p domain.Policy) (int64, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return 0, fmt.Errorf("нужно имя фильтра")
	}
	p.Kind = domain.NormalizeGuardKind(p.Kind)
	p.Action = domain.NormalizeGuardAction(p.Action)
	p.Mode = domain.NormalizeGuardMode(p.Mode)
	if p.Kind == domain.GuardSystemPrompt {
		p.Mode = domain.GuardPre
		p.Action = domain.GuardBlock
	}
	en := 0
	if p.Enabled {
		en = 1
	}
	cfg := p.ConfigJSON()
	if p.ID > 0 {
		_, err := s.DB.Exec(`UPDATE security_policies SET name=?, kind=?, action=?, mode=?, enabled=?, config=? WHERE id=?`,
			p.Name, p.Kind, p.Action, p.Mode, en, cfg, p.ID)
		if err != nil {
			return 0, err
		}
		if p.Targets != nil {
			if err := s.SetPolicyTargets(p.ID, p.Targets); err != nil {
				return 0, err
			}
		}
		return p.ID, nil
	}
	res, err := s.DB.Exec(`INSERT INTO security_policies (name, kind, action, mode, enabled, config) VALUES (?,?,?,?,?,?)`,
		p.Name, p.Kind, p.Action, p.Mode, en, cfg)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if p.Targets != nil {
		if err := s.SetPolicyTargets(id, p.Targets); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (s *Store) DeletePolicy(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM security_policies WHERE id=?`, id)
	return err
}

func (s *Store) SetPolicyTargets(id int64, targets []domain.PolicyTarget) error {
	if _, err := s.DB.Exec(`DELETE FROM security_targets WHERE policy_id=?`, id); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, t := range targets {
		kind := strings.TrimSpace(t.Kind)
		key := strings.TrimSpace(t.Key)
		if kind == "" || key == "" {
			continue
		}
		if kind != domain.GuardTargetAlias && kind != domain.GuardTargetQueue && kind != domain.GuardTargetModel {
			continue
		}
		sig := kind + "\x00" + key
		if seen[sig] {
			continue
		}
		seen[sig] = true
		if _, err := s.DB.Exec(`INSERT INTO security_targets (policy_id, target_kind, target_key) VALUES (?,?,?)`, id, kind, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PoliciesFor(alias, queueAlias, upstream string) ([]domain.Policy, error) {
	alias = strings.TrimSpace(alias)
	queueAlias = strings.TrimSpace(queueAlias)
	upstream = strings.TrimSpace(upstream)
	if alias == "" && queueAlias == "" && upstream == "" {
		return nil, nil
	}
	rows, err := s.DB.Query(`
SELECT DISTINCT p.id, p.name, p.kind, p.action, p.mode, p.enabled, p.config
FROM security_policies p
JOIN security_targets t ON t.policy_id = p.id
WHERE p.enabled=1 AND (
  (? != '' AND t.target_kind='alias' AND t.target_key=?) OR
  (? != '' AND t.target_kind='queue' AND t.target_key=?) OR
  (? != '' AND t.target_kind='model' AND t.target_key=?)
)
ORDER BY p.id`, alias, alias, queueAlias, queueAlias, upstream, upstream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Policy
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPolicy(row rowScanner) (domain.Policy, error) {
	var p domain.Policy
	var en int
	var cfg string
	if err := row.Scan(&p.ID, &p.Name, &p.Kind, &p.Action, &p.Mode, &en, &cfg); err != nil {
		return p, err
	}
	p.Enabled = en == 1
	p.Config = domain.ParsePolicyConfig(cfg)
	return p, nil
}

func (s *Store) allTargets() (map[int64][]domain.PolicyTarget, error) {
	rows, err := s.DB.Query(`SELECT policy_id, target_kind, target_key FROM security_targets ORDER BY policy_id, target_kind, target_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]domain.PolicyTarget{}
	for rows.Next() {
		var id int64
		var t domain.PolicyTarget
		if err := rows.Scan(&id, &t.Kind, &t.Key); err != nil {
			return nil, err
		}
		out[id] = append(out[id], t)
	}
	return out, rows.Err()
}
