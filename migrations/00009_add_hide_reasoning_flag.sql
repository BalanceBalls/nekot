-- +goose Up
-- +goose StatementBegin
ALTER TABLE settings ADD COLUMN hide_reasoning INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE settings DROP COLUMN hide_reasoning;
-- +goose StatementEnd
