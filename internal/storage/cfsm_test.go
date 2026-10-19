package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestCFSMConfigMigrationPreservesExistingData(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(fmt.Sprintf("foreign_keys_%t", foreignKeys), func(t *testing.T) { testCFSMConfigMigration(t, foreignKeys) })
	}
}

func testCFSMConfigMigration(t *testing.T, foreignKeys bool) {
	path := filepath.Join(t.TempDir(), "existing.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE probe_configs (
 id INTEGER PRIMARY KEY CHECK(id=1), probe_type TEXT NOT NULL CHECK(probe_type IN ('nezha','nezhav0','dstatus','komari')),
 address TEXT NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
 INSERT INTO probe_configs(id,probe_type,address) VALUES(1,'komari','https://old.example');
 CREATE TABLE probe_servers (
 id INTEGER PRIMARY KEY AUTOINCREMENT, config_id INTEGER NOT NULL REFERENCES probe_configs(id) ON DELETE CASCADE,
 server_id TEXT NOT NULL, name TEXT NOT NULL, traffic_method TEXT NOT NULL, monthly_traffic_bytes INTEGER NOT NULL DEFAULT 0,
 position INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(config_id,server_id));
 INSERT INTO probe_servers(id,config_id,server_id,name,traffic_method,monthly_traffic_bytes,position) VALUES(42,1,'existing-uuid','existing','up',12345,3);
 CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated VALUES('keep');
 CREATE INDEX custom_probe_address ON probe_configs(address);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if foreignKeys {
		path += "?_pragma=foreign_keys(1)"
	}
	repo, err := NewTrafficRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	var fk int
	if err := repo.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if (fk == 1) != foreignKeys {
		t.Fatalf("foreign_keys setting not restored: %d", fk)
	}
	cfg, err := repo.GetProbeConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProbeType != "komari" || cfg.Address != "https://old.example" || len(cfg.Servers) != 1 || cfg.Servers[0].ID != 42 || cfg.Servers[0].MonthlyTrafficBytes != 12345 || cfg.Servers[0].Position != 3 {
		t.Fatalf("migration lost data: %+v", cfg)
	}
	var value string
	if err := repo.db.QueryRow("SELECT value FROM unrelated").Scan(&value); err != nil || value != "keep" {
		t.Fatalf("unrelated: %q %v", value, err)
	}
	if _, err := repo.db.Exec("UPDATE probe_configs SET probe_type='cfsm'"); err != nil {
		t.Fatalf("CFSM schema support missing: %v", err)
	}
	var count int
	repo.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='custom_probe_address'").Scan(&count)
	if count != 1 {
		t.Fatal("custom index lost")
	}
	if err := repo.migrate(); err != nil {
		t.Fatalf("idempotence: %v", err)
	}
	cfg.ProbeType = "cfsm"
	if _, err := repo.UpsertProbeConfig(context.Background(), cfg); err != nil {
		t.Fatalf("CFSM validation: %v", err)
	}
}
