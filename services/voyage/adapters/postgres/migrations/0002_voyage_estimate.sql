-- +goose Up
-- +goose StatementBegin
-- The authoritative estimate, applied asynchronously when estimate.ready is
-- consumed (Phase 3). NULL means "not yet estimated" (pending).
ALTER TABLE voyages ADD COLUMN estimate_minor BIGINT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE voyages DROP COLUMN estimate_minor;
-- +goose StatementEnd
