-- +goose Up
-- +goose StatementBegin
CREATE TABLE mcp_server_state (
    server_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE mcp_server_state;
-- +goose StatementEnd
