-- +goose Up
-- +goose StatementBegin
-- voyages: one row per voyage. client_request_id is the primary key, which is
-- also the unique idempotency index — CreateVoyage retries collide here and
-- return the existing row (Phase 2) rather than inserting a duplicate.
CREATE TABLE voyages (
    client_request_id TEXT        PRIMARY KEY,
    origin            TEXT        NOT NULL,
    dest              TEXT        NOT NULL,
    distance_nm       INTEGER     NOT NULL,
    fees_minor        BIGINT      NOT NULL,
    version           BIGINT      NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
-- outbox: the transactional outbox (D-005). Rows are written in the same
-- transaction as the voyage they describe; a poller publishes them and stamps
-- published_at (flag-on-ack). published_at IS NULL means "not yet published".
CREATE TABLE outbox (
    id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic        TEXT        NOT NULL,
    payload      BYTEA       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS outbox;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS voyages;
-- +goose StatementEnd
