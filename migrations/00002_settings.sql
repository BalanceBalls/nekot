-- +goose Up
-- +goose StatementBegin
CREATE TABLE settings (
  settings_id INTEGER PRIMARY KEY,
  settings_model VARCHAR(255) NOT NULL,
  settings_max_tokens REAL NOT NULL, -- Changed DECIMAL to REAL
  settings_frequency REAL NOT NULL -- Changed DECIMAL to REAL
);

INSERT INTO settings 
(settings_id, settings_model, settings_max_tokens, settings_frequency) 
VALUES (0, 'gpt-3.5-turbo', 3000.0, 0.0); 

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP TABLE settings;
-- +goose StatementEnd
