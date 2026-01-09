-- +goose Up
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN web_search_enabled INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE settings DROP COLUMN web_search_enabled;
-- +goose StatementEnd
