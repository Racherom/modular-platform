//go:build integration

package store

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestMigrationStacks covers spec 0027, AC-5: migration 00002 renames the tables, columns, index
// and constraints of the first migration and keeps every row, link and digest; its down block
// brings the old names back. The old names below are the point of this test (vocabulary: allow).
func TestMigrationStacks(t *testing.T) {
	ctr, err := postgres.Run(t.Context(), "postgres:18.6-alpine3.24",
		postgres.WithDatabase("platform_api"), postgres.WithUsername("api"), postgres.WithPassword("test-password"),
		postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := ctr.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	p, err := goose.NewProvider(goose.DialectPostgres, db, mustSub(migrations, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if _, err := p.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}

	// two stacks with one operation each, under the names of 00001
	for _, row := range [][2]string{{"one", "sha256:1"}, {"two", "sha256:2"}} {
		var stack, op string
		if err := pool.QueryRow(ctx, `INSERT INTO deployments (name) VALUES ($1) RETURNING id`, row[0]).Scan(&stack); err != nil { // vocabulary: allow
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO operations (deployment_id, action, status, graph, plan_digest, -- vocabulary: allow
			started_by_iss, started_by_sub, started_by_email) VALUES ($1, 'apply', 'succeeded', '{"name":"x"}', $2, 'iss', 'sub', 'a@b.c')
			RETURNING id`, stack, row[1]).Scan(&op); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE deployments SET latest_operation_id = $1 WHERE id = $2`, op, stack); err != nil { // vocabulary: allow
			t.Fatal(err)
		}
	}
	before := stackRows(ctx, t, db, `SELECT d.name, d.project, o.plan_digest, d.latest_operation_id = o.id -- vocabulary: allow
		FROM deployments d JOIN operations o ON o.deployment_id = d.id ORDER BY d.name`) // vocabulary: allow

	if _, err := p.UpTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	after := stackRows(ctx, t, db, `SELECT s.name, s.compose_project, o.digest, s.latest_operation_id = o.id
		FROM stacks s JOIN operations o ON o.stack_id = s.id ORDER BY s.name`)
	want := []string{"one mp-one sha256:1 true", "two mp-two sha256:2 true"}
	if !slices.Equal(before, want) || !slices.Equal(after, want) {
		t.Fatalf("rows before %q, after %q, want %q", before, after, want)
	}
	wantNew := []string{
		"operations_one_running_idx", "operations_pkey", "operations_rollback_of_fkey", "operations_stack_id_fkey", "operations_stack_started_idx",
		"stacks_compose_project_key", "stacks_latest_operation_fk", "stacks_name_check", "stacks_name_key", "stacks_pkey",
	}
	if got := schemaNames(ctx, t, db); !slices.Equal(got, wantNew) {
		t.Errorf("names after up = %q, want %q", got, wantNew)
	}

	if _, err := p.DownTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	wantOld := []string{
		"deployments_latest_operation_fk", "deployments_name_check", "deployments_name_key", "deployments_pkey", // vocabulary: allow
		"deployments_project_key", "operations_deployment_id_fkey", "operations_deployment_started_idx", // vocabulary: allow
		"operations_one_running_idx", "operations_pkey", "operations_rollback_of_fkey",
	}
	if got := schemaNames(ctx, t, db); !slices.Equal(got, wantOld) {
		t.Errorf("names after down = %q, want %q", got, wantOld)
	}
	if again := stackRows(ctx, t, db, `SELECT d.name, d.project, o.plan_digest, d.latest_operation_id = o.id -- vocabulary: allow
		FROM deployments d JOIN operations o ON o.deployment_id = d.id ORDER BY d.name`); !slices.Equal(again, want) { // vocabulary: allow
		t.Errorf("rows after down %q, want %q", again, want)
	}
}

// stackRows runs query and joins each row's four columns into one string.
func stackRows(ctx context.Context, t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, composeProject, digest string
		var linked bool
		if err := rows.Scan(&name, &composeProject, &digest, &linked); err != nil {
			t.Fatal(err)
		}
		out = append(out, name+" "+composeProject+" "+digest+" "+map[bool]string{true: "true", false: "false"}[linked])
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// schemaNames lists the constraint and index names of the stack and operation tables, sorted.
func schemaNames(ctx context.Context, t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT conname FROM pg_constraint c JOIN pg_class r ON r.oid = c.conrelid
			WHERE r.relname IN ('stacks', 'deployments', 'operations') AND c.contype IN ('p', 'u', 'f', 'c') -- vocabulary: allow
			AND conname NOT LIKE 'operations_%check'
		UNION SELECT indexname FROM pg_indexes WHERE tablename IN ('stacks', 'deployments', 'operations') -- vocabulary: allow
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
