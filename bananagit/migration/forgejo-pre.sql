-- Run once on a Forgejo 16 database BEFORE BananaGit's first start.
-- Works on SQLite and PostgreSQL. See bananagit/docs/migrating-from-forgejo.md.

-- Forgejo dropped these two columns; Gitea's migration 309 cannot add the
-- NOT NULL one to a table that has rows, and stops the server from starting.
ALTER TABLE notification ADD COLUMN updated_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE notification ADD COLUMN commit_id VARCHAR(255) NULL;

-- Webhooks of Forgejo's own type are Gitea-format webhooks.
UPDATE webhook SET type = 'gitea' WHERE type = 'forgejo';

-- Forgejo's themes do not exist here: fall back to the default theme.
UPDATE "user" SET theme = '' WHERE theme LIKE 'forgejo-%';
