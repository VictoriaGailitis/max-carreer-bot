package catalog

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct{ pool *pgxpool.Pool }

var ErrNoActiveCatalog = errors.New("no active catalog")

func NewStorage(pool *pgxpool.Pool) (*Storage, error) {
	if pool == nil {
		return nil, errors.New("missing catalog database")
	}
	return &Storage{pool: pool}, nil
}

// Import holds one transaction-level advisory lock for catalog publication.
// A failed insert leaves the previously active snapshot untouched.
func (s *Storage) Import(ctx context.Context, validated Validated) (version string, changed bool, err error) {
	if len(validated.Raw) == 0 {
		return "", false, errors.New("catalog was not validated")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(845007321)`); err != nil {
		return "", false, err
	}
	var activeHash []byte
	err = tx.QueryRow(ctx, `SELECT v.source_sha256 FROM catalog_active a JOIN catalog_versions v ON v.id=a.version_id WHERE a.is_demo=$1`, validated.Dataset.IsDemo).Scan(&activeHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	version = hex.EncodeToString(validated.Hash[:])
	if string(activeHash) == string(validated.Hash[:]) {
		return version, false, nil
	}
	var versionID int64
	err = tx.QueryRow(ctx, `SELECT id FROM catalog_versions WHERE source_sha256=$1`, validated.Hash[:]).Scan(&versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO catalog_versions (source_sha256, is_demo) VALUES ($1,$2) RETURNING id`, validated.Hash[:], validated.Dataset.IsDemo).Scan(&versionID)
		if err != nil {
			return "", false, err
		}
		for _, item := range validated.Dataset.Items {
			payload, err := json.Marshal(item)
			if err != nil {
				return "", false, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO opportunities (catalog_version_id,id,payload,publication_status,availability,is_demo) VALUES ($1,$2,$3,$4,$5,$6)`,
				versionID, item.ID, payload, item.PublicationStatus, item.Availability, item.IsDemo)
			if err != nil {
				return "", false, fmt.Errorf("import opportunity %s: %w", item.ID, err)
			}
		}
	} else if err != nil {
		return "", false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO catalog_active (is_demo,version_id) VALUES ($1,$2)
		ON CONFLICT (is_demo) DO UPDATE SET version_id=EXCLUDED.version_id`, validated.Dataset.IsDemo, versionID)
	if err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return version, true, nil
}

func (s *Storage) Active(ctx context.Context, demo bool) (Stored, error) {
	var hash []byte
	var versionID int64
	err := s.pool.QueryRow(ctx, `SELECT v.id,v.source_sha256 FROM catalog_active a JOIN catalog_versions v ON v.id=a.version_id WHERE a.is_demo=$1`, demo).Scan(&versionID, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Stored{}, ErrNoActiveCatalog
	}
	if err != nil {
		return Stored{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT payload FROM opportunities WHERE catalog_version_id=$1 ORDER BY id`, versionID)
	if err != nil {
		return Stored{}, err
	}
	defer rows.Close()
	result := Stored{Version: hex.EncodeToString(hash), Items: make([]Opportunity, 0)}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return Stored{}, err
		}
		var item Opportunity
		if err := json.Unmarshal(raw, &item); err != nil {
			return Stored{}, err
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return Stored{}, err
	}
	return result, nil
}

func (s *Storage) Ready(ctx context.Context) error {
	if err := s.SchemaReady(ctx); err != nil {
		return err
	}
	var active bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_active WHERE is_demo=false)`).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrNoActiveCatalog
	}
	return nil
}

func (s *Storage) SchemaReady(ctx context.Context) error {
	var versions, active, items string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(to_regclass('public.catalog_versions')::text,''), COALESCE(to_regclass('public.catalog_active')::text,''), COALESCE(to_regclass('public.opportunities')::text,'')`).Scan(&versions, &active, &items); err != nil {
		return err
	}
	if versions == "" || active == "" || items == "" {
		return errors.New("catalog migration is missing")
	}
	return nil
}
