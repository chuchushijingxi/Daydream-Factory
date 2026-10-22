package main

import (
	"context"
	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/application/accounts"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

func main() {
	config := flag.String("config", "config/app.local.yaml", "private runtime configuration")
	secret := flag.String("password-file", "", "optional 0600 file for explicit operator maintenance initialization")
	printSQL := flag.Bool("print-sql", false, "print clean initialization SQL without connecting to a database")
	writeSQL := flag.String("write-sql", "", "write clean initialization SQL without connecting to a database")
	demoTeam := flag.Bool("demo-team", false, "on a fresh installation, also seed the public demo team (owner/admin/member) for trying the template")
	flag.Parse()
	if *printSQL || *writeSQL != "" {
		if *writeSQL != "" {
			if e := os.WriteFile(*writeSQL, []byte(postgres.InitializationSQL()), 0644); e != nil {
				log.Fatal(e)
			}
		} else {
			fmt.Print(postgres.InitializationSQL())
		}
		return
	}
	cfg, e := appconfig.Load(*config)
	if e != nil {
		log.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()
	store, e := postgres.Open(ctx, cfg.Database.URL, cfg.Database)
	if e != nil {
		log.Fatal(e)
	}
	defer store.Close()
	if e = store.Migrate(ctx); e != nil {
		// 迁移错误是 SQL 层的结构问题，不含凭据；带上原因便于排查旧库升级失败。
		log.Fatalf("identity migration failed: %v", e)
	}
	if *secret == "" {
		created, err := store.InitializeDefaultOperator(ctx)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("default operator initialization complete; created=%t; existing accounts unchanged", created)
		if *demoTeam {
			seeded, err := seedDemoTeam(ctx, store)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("demo team initialization complete; created=%t", seeded)
		}
		return
	}
	f, e := os.Open(*secret)
	if e != nil {
		log.Fatal("cannot open private password input")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 74 {
		log.Fatal("password input must be a 0600 regular file")
	}
	raw, e := io.ReadAll(io.LimitReader(f, 75))
	if e != nil {
		log.Fatal("cannot read private password input")
	}
	service := accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}}
	user, created, e := service.InitializeOperator(ctx, strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"))
	clear(raw)
	if e != nil {
		log.Fatal(e)
	}
	log.Printf("operator identity %s present; created=%t; existing passwords unchanged", user.Email, created)
}
