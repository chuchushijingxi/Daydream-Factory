package postgres

import (
	"bytes"
	"os"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestDistributableInitializationSQLMatchesCanonicalSchemaAndPassword(t *testing.T) {
	raw, err := os.ReadFile("../../../deploy/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte(InitializationSQL())) {
		t.Fatal("deploy/init.sql is stale; run go generate ./internal/adapter/postgres")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(initialOperatorPasswordHash), []byte("Daydream123456")); err != nil {
		t.Fatal("bootstrap hash does not match the documented public password")
	}
}
