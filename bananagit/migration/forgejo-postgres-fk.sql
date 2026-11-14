-- Run once on a Forgejo 16 PostgreSQL database, together with forgejo-pre.sql.
--
-- Forgejo added real foreign keys to its database. Gitea and BananaGit do not
-- use them: constraints that reference repository make PostgreSQL reject the
-- repository DELETE (error 23503) with an HTTP 500, and rows in Forgejo-only
-- tables such as access_token_resource_repo are never removed by BananaGit.
-- Dropping these constraints restores the schema BananaGit expects.
--
-- SQLite does not need this file and does not support DROP CONSTRAINT.

ALTER TABLE IF EXISTS access DROP CONSTRAINT IF EXISTS access_repo_id_fkey;
ALTER TABLE IF EXISTS access_token_resource_repo DROP CONSTRAINT IF EXISTS access_token_resource_repo_repo_id_fkey;
ALTER TABLE IF EXISTS collaboration DROP CONSTRAINT IF EXISTS collaboration_repo_id_fkey;
ALTER TABLE IF EXISTS action_runner_token DROP CONSTRAINT IF EXISTS action_runner_token_repo_id_fkey;
ALTER TABLE IF EXISTS action_user DROP CONSTRAINT IF EXISTS action_user_repo_id_fkey;
ALTER TABLE IF EXISTS pull_request DROP CONSTRAINT IF EXISTS pull_request_base_repo_id_fkey;
ALTER TABLE IF EXISTS authorized_integ_resource_repo DROP CONSTRAINT IF EXISTS authorized_integ_resource_repo_repo_id_fkey;
