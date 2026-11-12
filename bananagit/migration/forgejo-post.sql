-- Run once AFTER BananaGit's first start has migrated the database.
-- Works on SQLite and PostgreSQL.

-- Carry over users blocked by other users (Forgejo's table to Gitea's).
INSERT INTO user_blocking (blocker_id, blockee_id, note, created_unix)
SELECT b.user_id, b.block_id, '', b.created_unix FROM forgejo_blocked_user b
WHERE NOT EXISTS (SELECT 1 FROM user_blocking u WHERE u.blocker_id = b.user_id AND u.blockee_id = b.block_id);
