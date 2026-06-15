-- +goose Up
-- +goose StatementBegin
-- The W3C trace context captured when the outbox row was written, so the poller
-- can publish the event under the original request's trace (the outbox would
-- otherwise break trace continuity across the async boundary).
ALTER TABLE outbox ADD COLUMN trace_carrier JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE outbox DROP COLUMN trace_carrier;
-- +goose StatementEnd
