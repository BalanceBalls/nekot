package mcpclient

import (
	"database/sql"
	"fmt"
)

func loadEnabledState(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT server_id, enabled FROM mcp_server_state`)
	if err != nil {
		return nil, fmt.Errorf("load MCP server state: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var id string
		var enabled bool
		if err := rows.Scan(&id, &enabled); err != nil {
			return nil, fmt.Errorf("scan MCP server state: %w", err)
		}
		result[id] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate MCP server state: %w", err)
	}
	return result, nil
}

func saveEnabledState(db *sql.DB, id string, enabled bool) error {
	_, err := db.Exec(`
		INSERT INTO mcp_server_state (server_id, enabled, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(server_id) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = CURRENT_TIMESTAMP
	`, id, enabled)
	if err != nil {
		return fmt.Errorf("save MCP server state for %q: %w", id, err)
	}
	return nil
}

func deleteEnabledState(db *sql.DB, id string) error {
	if _, err := db.Exec(`DELETE FROM mcp_server_state WHERE server_id = ?`, id); err != nil {
		return fmt.Errorf("delete MCP server state for %q: %w", id, err)
	}
	return nil
}
