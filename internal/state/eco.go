package state

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertECO persists an engineering change order to the database.
func (db *DB) InsertECO(eco *EcoEntry) error {
	result, err := db.conn.Exec(
		`INSERT INTO eco_log (eco_code, category, description, affected_refs, proposed_change, status, approved_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		eco.EcoCode, eco.Category, eco.Description, eco.AffectedRefs,
		eco.ProposedChange, eco.Status, nullString(eco.ApprovedBy), eco.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert eco: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		eco.ID = id
	}
	return nil
}

// UpdateECOStatus updates the status of an ECO and optionally sets the approver.
func (db *DB) UpdateECOStatus(id int64, status string, approvedBy string) error {
	now := time.Now()
	_, err := db.conn.Exec(
		`UPDATE eco_log SET status = ?, approved_by = ?, resolved_at = ? WHERE id = ?`,
		status, nullString(approvedBy), now, id,
	)
	if err != nil {
		return fmt.Errorf("update eco status: %w", err)
	}
	return nil
}

// ListECOs returns ECO entries filtered by status. If status is empty, all entries are returned.
func (db *DB) ListECOs(status string) ([]*EcoEntry, error) {
	query := `SELECT id, eco_code, category, description, affected_refs, proposed_change, status, approved_by, created_at, resolved_at FROM eco_log`
	var args []interface{}

	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list ecos: %w", err)
	}
	defer rows.Close()

	var ecos []*EcoEntry
	for rows.Next() {
		eco := &EcoEntry{}
		var approvedBy sql.NullString
		err := rows.Scan(
			&eco.ID, &eco.EcoCode, &eco.Category, &eco.Description,
			&eco.AffectedRefs, &eco.ProposedChange, &eco.Status,
			&approvedBy, &eco.CreatedAt, &eco.ResolvedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan eco: %w", err)
		}
		eco.ApprovedBy = approvedBy.String
		ecos = append(ecos, eco)
	}
	return ecos, rows.Err()
}
