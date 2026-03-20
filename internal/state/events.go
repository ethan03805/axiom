package state

import (
	"database/sql"
	"fmt"
	"time"
)

// Event represents a persisted system event.
type Event struct {
	ID        int64
	Type      string
	TaskID    string
	AgentType string
	AgentID   string
	Details   string
	Timestamp time.Time
}

// EventFilter defines filtering criteria for listing events.
type EventFilter struct {
	EventType string
	TaskID    string
	Since     *time.Time
}

// InsertEvent persists an event to the database.
func (db *DB) InsertEvent(event *Event) error {
	db.wmu.Lock()
	result, err := db.conn.Exec(
		`INSERT INTO events (event_type, task_id, agent_type, agent_id, details, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		event.Type, nullString(event.TaskID),
		nullString(event.AgentType), nullString(event.AgentID),
		event.Details, event.Timestamp,
	)
	db.wmu.Unlock()
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		event.ID = id
	}
	return nil
}

// ListEvents returns events matching the given filter criteria.
func (db *DB) ListEvents(filter EventFilter) ([]*Event, error) {
	query := `SELECT id, event_type, task_id, agent_type, agent_id, details, timestamp FROM events WHERE 1=1`
	var args []interface{}

	if filter.EventType != "" {
		query += " AND event_type = ?"
		args = append(args, filter.EventType)
	}
	if filter.TaskID != "" {
		query += " AND task_id = ?"
		args = append(args, filter.TaskID)
	}
	if filter.Since != nil {
		query += " AND timestamp >= ?"
		args = append(args, *filter.Since)
	}

	query += " ORDER BY timestamp"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		ev := &Event{}
		var taskID, agentType, agentID sql.NullString
		err := rows.Scan(
			&ev.ID, &ev.Type, &taskID, &agentType, &agentID,
			&ev.Details, &ev.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.TaskID = taskID.String
		ev.AgentType = agentType.String
		ev.AgentID = agentID.String
		events = append(events, ev)
	}
	return events, rows.Err()
}
