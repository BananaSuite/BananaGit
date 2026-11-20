// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package db

import (
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
)

// Foreign keys in Gitea's schema, as identifiers PostgreSQL has already quoted.
const listForeignKeysPostgres = `SELECT c.conrelid::regclass::text AS tbl, quote_ident(c.conname) AS con
FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace
WHERE c.contype = 'f' AND n.nspname = current_schema()
ORDER BY 1, 2`

// DropForeignKeys drops every foreign key in Gitea's schema (BananaGit). Gitea never creates
// them and its writes and migrations do not respect them, so any found was left by Forgejo:
// on a migrated database they reject writes with error 23503 (an HTTP 500). Failures are only
// logged. SQLite is skipped: it does not enforce foreign keys unless asked to.
func DropForeignKeys(e Engine) {
	if !setting.Database.Type.IsPostgreSQL() {
		return
	}
	rows, err := e.Query(listForeignKeysPostgres)
	if err != nil {
		log.Error("BananaGit: could not list foreign keys: %v", err)
		return
	}
	for _, row := range rows {
		table, constraint := string(row["tbl"]), string(row["con"])
		if _, err := e.Exec("ALTER TABLE " + table + " DROP CONSTRAINT IF EXISTS " + constraint); err != nil { // both quoted by PostgreSQL
			log.Error("BananaGit: could not drop foreign key %s on %s: %v", constraint, table, err)
			continue
		}
		log.Info("BananaGit: dropped foreign key %s on %s (left over from Forgejo; Gitea does not use foreign keys)", constraint, table)
	}
}
