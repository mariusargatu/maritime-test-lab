-- RED self-test fixture for the DB DDL breaking-change gate (D-049).
--
-- NOT a real migration. It lives under testdata/ so goose's `migrations/*.sql`
-- embed (non-recursive) never loads it, and Go tooling ignores testdata/ by
-- convention. Its Up block drops a live column — squawk MUST reject it. If the
-- gate ever lets this through it is decoration, which the lab's prime directive
-- forbids: every gate ships a proof it can fail.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE voyages DROP COLUMN estimate_minor;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE voyages ADD COLUMN estimate_minor BIGINT;
-- +goose StatementEnd
