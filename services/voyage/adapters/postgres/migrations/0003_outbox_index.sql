-- +goose Up
-- +goose StatementBegin
-- The outbox poller scans for unpublished rows (published_at IS NULL). A partial
-- index keeps that scan cheap as the published audit trail grows.
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_outbox_unpublished;
-- +goose StatementEnd
