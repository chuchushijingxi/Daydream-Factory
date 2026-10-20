// smtp-import performs the controlled one-time transition; it never sends mail.
package main

import (
	"context"
	"drama-agent/internal/adapter/mailmigration"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/mailconfig"
	"flag"
	"log"
)

func main() {
	path := flag.String("config", "config/app.local.yaml", "0600 legacy configuration to migrate")
	flag.Parse()
	document, err := mailmigration.Read(*path)
	if err != nil {
		log.Fatal(err)
	}
	if document.Legacy == nil {
		log.Print("no legacy SMTP section; runtime configuration unchanged")
		return
	}
	cfg := document.Config
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()
	store, err := postgres.Open(ctx, cfg.Database.URL, cfg.Database)
	if err != nil {
		log.Fatal("migration database unavailable")
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		log.Fatal("additive mail schema migration failed; original YAML retained")
	}
	cipher, err := store.ConfigurationCipher(ctx, cfg.Models.KeyFile)
	if err != nil {
		log.Fatal("original configuration encryption key unavailable; original YAML retained")
	}
	service := mailconfig.Service{Store: store, Cipher: cipher}
	if _, err = service.ImportLegacy(ctx, document.Legacy.Settings, document.Legacy.Password); err != nil {
		log.Fatal("mail migration not confirmed; original YAML retained")
	}
	if err = document.Finalize(); err != nil {
		log.Fatal("PG configuration verified; original YAML retained, rerun importer after checking file changes")
	}
	log.Print("SMTP encrypted in PG and verified; legacy YAML section removed; no mail submitted")
}
