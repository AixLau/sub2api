// credential-control performs offline single-host Compose cutover. It does not
// synthesize identity verification and never starts old containers after fencing.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
)

func main() {
	operation := flag.String("operation", "preview", "preview, migrate, canary, rollback")
	project := flag.String("compose-project", "", "exact Compose project to fence")
	gateway := flag.String("gateway-service", "", "exact gateway service to fence")
	account := flag.Int64("account", 0, "existing account ID")
	principal := flag.Int64("principal", 0, "principal ID")
	actor := flag.Int64("actor", 0, "administrator user ID")
	version := flag.Int64("version", 0, "expected config version")
	total := flag.Int("total", -1, "explicit total concurrency")
	importID := flag.String("import", "", "verified credential import reference")
	opID := flag.String("operation-id", "", "UUID for migration idempotency")
	evidence := flag.String("drain-evidence", "", "audited evidence old executions/bindings were drained")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, err := sql.Open("postgres", os.Getenv("SUB2API_CREDENTIAL_CONTROL_DSN"))
	if err != nil {
		fail()
	}
	defer db.Close()
	if db.PingContext(ctx) != nil {
		fail()
	}
	if *operation == "preview" {
		v, err := repository.NewCredentialMigrationStore(db).PreviewCredentialMigration(ctx, []int64{*account})
		if err != nil {
			fail()
		}
		_ = json.NewEncoder(os.Stdout).Encode(v)
		return
	}
	vault, err := service.NewCredentialVault(os.Getenv("SUB2API_CREDENTIAL_VAULT_KEY"))
	if err != nil {
		fail()
	}
	rollout := repository.NewCredentialRollout(db, vault, credentialfence.Docker{Project: *project, Service: *gateway})
	switch *operation {
	case "migrate":
		var id int64
		id, err = rollout.Migrate(ctx, repository.CredentialMigrationInput{AccountID: *account, ActorID: *actor, ImportID: *importID, OperationID: *opID, DrainEvidence: *evidence, RequestedLimit: *total})
		if err == nil {
			fmt.Printf("{\"principal_id\":%d,\"state\":\"PAUSED\"}\n", id)
		}
	case "canary":
		err = rollout.Canary(ctx, *actor, *principal, *version)
	case "rollback":
		err = rollout.Rollback(ctx, *actor, *principal, *version)
	default:
		fail()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "credential control rejected; inspect audited preconditions (no credentials logged)")
		os.Exit(1)
	}
}
func fail() {
	fmt.Fprintln(os.Stderr, "credential control unavailable; check configuration and authenticated prerequisites")
	os.Exit(1)
}
