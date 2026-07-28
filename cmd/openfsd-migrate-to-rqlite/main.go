// Command openfsd-migrate-to-rqlite copies users and config rows from a local
// SQLite file into an rqlite cluster via HTTP. Schema must already be applied
// (openfsd with DATABASE_MIGRATE_LEADER=true against rqlite, or MigrateRqlite).
//
// Usage:
//
//	openfsd-migrate-to-rqlite -sqlite openfsd.db -rqlite http://127.0.0.1:4001
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/renorris/openfsd/internal/db"
	_ "modernc.org/sqlite"
)

func main() {
	sqliteDSN := flag.String("sqlite", "openfsd.db", "source SQLite DSN/path")
	rqliteURL := flag.String("rqlite", "http://127.0.0.1:4001", "destination rqlite HTTP base URL")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := migrate(ctx, *sqliteDSN, *rqliteURL); err != nil {
		log.Fatal(err)
	}
	fmt.Println("migrate-to-rqlite: OK")
}

func migrate(ctx context.Context, sqliteDSN, rqliteURL string) error {
	sqlDB, err := sql.Open("sqlite", sqliteDSN)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := sqlDB.PingContext(ctx); err != nil {
		return err
	}
	src, err := db.NewRepositories(sqlDB)
	if err != nil {
		return err
	}

	client := db.NewRqliteClient(rqliteURL, db.RqliteClientOptions{Timeout: 30 * time.Second})
	if err := client.Ping(ctx); err != nil {
		return fmt.Errorf("rqlite ping: %w", err)
	}
	dstUsers := db.NewRqliteUserRepository(client, db.ReadStrong)
	dstCfg := db.NewRqliteConfigRepository(client, db.ReadStrong)

	// Copy users page by page
	offset := 0
	for {
		users, err := src.UserRepo.ListUsers(ctx, db.UserListFilter{Limit: 100, Offset: offset})
		if err != nil {
			return err
		}
		if len(users) == 0 {
			break
		}
		for _, u := range users {
			// List omits password — fetch full row
			full, err := src.UserRepo.GetUserByCID(ctx, u.CID)
			if err != nil {
				return err
			}
			// Insert via execute with explicit CID (bypass CreateUser hash re-entry):
			// re-use CreateUser would re-hash. Copy hash directly.
			_, err = client.Execute(ctx, `
				INSERT INTO users (cid, password, first_name, last_name, network_rating, pilot_rating)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(cid) DO UPDATE SET
					password=excluded.password,
					first_name=excluded.first_name,
					last_name=excluded.last_name,
					network_rating=excluded.network_rating,
					pilot_rating=excluded.pilot_rating`,
				full.CID, full.Password, full.FirstName, full.LastName, full.NetworkRating, full.PilotRating,
			)
			if err != nil {
				// Fallback without explicit cid if table uses AUTOINCREMENT only
				_ = dstUsers
				return fmt.Errorf("user cid %d: %w", full.CID, err)
			}
		}
		offset += len(users)
	}

	// Config keys
	for _, key := range []string{
		db.ConfigJwtSecretKey, db.ConfigWelcomeMessage,
		db.ConfigFsdServerHostname, db.ConfigFsdServerIdent, db.ConfigFsdServerLocation,
		db.ConfigApiServerBaseURL, db.ConfigRequirePilotPPL,
	} {
		v, err := src.ConfigRepo.Get(ctx, key)
		if err != nil {
			continue
		}
		if err := dstCfg.Set(ctx, key, v); err != nil {
			return fmt.Errorf("config %s: %w", key, err)
		}
	}
	return nil
}

func init() {
	log.SetOutput(os.Stderr)
}
