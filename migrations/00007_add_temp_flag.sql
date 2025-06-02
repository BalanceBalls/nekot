-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN is_temporary INTEGER NOT NULL DEFAULT 0; 
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN is_temporary;
-- +goose StatementEnd
