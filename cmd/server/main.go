package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/einoagent"
	"drama-agent/internal/adapter/httpapi"
	"drama-agent/internal/adapter/imagethumbnail"
	modeladapter "drama-agent/internal/adapter/model"
	"drama-agent/internal/adapter/notificationmarkdown"
	"drama-agent/internal/adapter/passwordhash"
	"drama-agent/internal/adapter/postgres"
	"drama-agent/internal/adapter/redisstate"
	"drama-agent/internal/adapter/s3objects"
	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/adapter/smtpmail"
	"drama-agent/internal/adapter/stylecatalog"
	"drama-agent/internal/adapter/taskruntime"
	"drama-agent/internal/application/accounts"
	creditsapp "drama-agent/internal/application/credits"
	"drama-agent/internal/application/mailconfig"
	"drama-agent/internal/application/medialibrary"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/modelusage"
	"drama-agent/internal/application/notifications"
	"drama-agent/internal/application/platform"
	"drama-agent/internal/application/ports"
	app "drama-agent/internal/application/project"
	uploadapp "drama-agent/internal/application/upload"
	md "drama-agent/internal/domain/modelconfig"
)

func main() {
	configFile := flag.String("config", "config/app.local.yaml", "private runtime YAML file")
	readOnlyFlag := flag.Bool("read-only", false, "browse existing data without writes or task execution")
	listen := flag.String("listen", appconfig.Defaults().HTTP.Address, "HTTP bind address (container deployment uses 0.0.0.0:8787)")
	flag.Parse()
	cfg, err := appconfig.Load(*configFile)
	if err != nil {
		log.Fatal(err)
	}
	cfg.ReadOnly = *readOnlyFlag
	cfg.HTTP.Address = *listen
	ctx, cancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
	defer cancel()
	readOnly := cfg.ReadOnly
	runtimeState, err := redisstate.Open(ctx, cfg.Redis)
	if err != nil {
		log.Fatal(err)
	}
	defer runtimeState.Close()
	database := cfg.Database.URL
	if readOnly {
		parsed, e := url.Parse(database)
		if e != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
			log.Fatal("read-only mode requires a PostgreSQL URL")
		}
		query := parsed.Query()
		query.Set("default_transaction_read_only", "on")
		parsed.RawQuery = query.Encode()
		database = parsed.String()
	}
	store, err := postgres.Open(ctx, database, cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	store.UseRuntime(runtimeState)
	if !readOnly {
		if err = store.Migrate(ctx); err != nil {
			log.Fatal(err)
		}
	}
	var credentialCipher ports.CredentialCipher
	if !readOnly {
		credentialCipher, err = store.ConfigurationCipher(ctx, cfg.Models.KeyFile)
		if err != nil {
			log.Fatal("cannot load original configuration encryption key")
		}

	}
	cancel()
	configuration := &modelconfig.Service{Cache: runtimeState, Store: store, Projects: store, Cipher: credentialCipher, ReadOnly: readOnly, Logos: store, Directory: func(ctx context.Context, key, endpoint, protocol string) ([]ports.RemoteModel, error) {
		ctx, cancel := context.WithTimeout(ctx, cfg.Models.DirectoryTimeout)
		defer cancel()
		return modeladapter.FetchModelDirectory(ctx, key, endpoint, protocol)
	}, Factory: func(ctx context.Context, key, endpoint, protocol string, profile md.ConfiguredModel) (ports.Model, error) {
		model, err := modeladapter.NewConfigured(ctx, key, endpoint, protocol, profile, cfg.Models)
		if err != nil {
			return nil, err
		}
		return runtimeState.LimitModel(model, protocol, endpoint, key), nil
	}}
	service := &app.Service{Credits: store, Prices: configuration, Store: store, Agent: einoagent.New(), Models: configuration, Importer: scriptimport.Importer{}, Authorize: app.UserInitiatedExecution, ReceiptTimeout: cfg.Project.ReceiptTimeout, TransportRetryInterval: cfg.Project.TransportRetryInterval}
	if !readOnly {
		service.Evidence = configuredEvidence(cfg.Evidence)
	}
	// 用户在工作台明确启动即可执行；开发期许可文件不再是正常使用的门槛。

	objectStore, err := s3objects.New(cfg.Storage)
	if err != nil {
		log.Fatal("cannot initialize S3 script storage")
	}
	if !readOnly {
		storageCtx, storageCancel := context.WithTimeout(context.Background(), cfg.StartupTimeout)
		err = objectStore.EnsureBucket(storageCtx)
		storageCancel()
		if err != nil {
			log.Fatal("cannot access private S3 script bucket")
		}
	}
	service.OriginalScripts = objectStore
	files := &uploadapp.Service{Store: store, Objects: objectStore, Thumbnails: imagethumbnail.FFmpeg{Settings: cfg.Media}, CommitCheckTimeout: cfg.Uploads.CommitCheckTimeout, CleanupTimeout: cfg.Uploads.CleanupTimeout}
	service.Files = files
	taskRuntime, taskQueue, err := taskruntime.New(cfg, store, objectStore, configuration, runtimeState, files)
	if err != nil {
		log.Fatal("cannot initialize task resources")
	}
	defer taskQueue.Close()
	configuration.Tasks = taskRuntime.Service
	service.MediaTasks = taskRuntime.Service
	taskRuntime.Service.Permit = &app.MediaCreditPermit{Ledger: store, Tasks: taskRuntime.Service.Store}

	demo := cfg.HTTP.DemoDir
	service.Styles, err = stylecatalog.LoadStyles(demo)
	if err != nil {
		log.Fatal("cannot load complete style catalog")
	}
	host, handler := httpapi.New(service, configuration, demo, 0)
	host.StreamHeartbeat = appconfig.StreamHeartbeat
	host.StreamWriteTimeout = appconfig.StreamWriteTimeout
	host.Runtime = runtimeState
	host.RequestsPerMinute = cfg.Redis.RequestsPerMinute
	host.IPRequestsPerMinute = cfg.Redis.IPRequestsPerMinute
	host.Workers.Updates = runtimeState
	host.Files = files
	accountService := &accounts.Service{Store: store, Hasher: passwordhash.Bcrypt{}, Thumbnails: files.Thumbnails}
	mailService := &mailconfig.Service{Store: store, Cipher: credentialCipher, Sender: smtpmail.Transport{}}
	accountService.Mailer = mailService
	host.MailConfig = mailService
	host.Accounts = accountService
	host.Authenticator = accountService
	host.Credits = &creditsapp.Service{Ledger: store, Queries: store, Updates: store, Users: store}
	host.Gifts = &creditsapp.Gifts{Store: store, Cache: runtimeState}
	host.Notifications = &notifications.Service{Store: store, Updates: store, Markdown: notificationmarkdown.Parser{}}
	host.Platform = &platform.Service{Store: store, ReadOnly: readOnly}
	host.Usage = &modelusage.Service{Queries: store}
	host.MediaLibrary = &medialibrary.Service{Store: store}
	host.ReadOnly = readOnly
	address := cfg.HTTP.Address
	if !readOnly {
		if err = host.Recover(context.Background()); err != nil {
			log.Fatal("cannot load running catalog projects")
		}
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server.BaseContext = func(net.Listener) context.Context { return signalCtx }
	workerCtx, stopWorker := context.WithCancel(signalCtx)
	defer stopWorker()
	if !readOnly {
		go store.RunRuntimeEvents(workerCtx)
	}
	go host.RunCreditRecovery(workerCtx, appconfig.CreditRecoveryInterval)
	if !readOnly {
		go runImageFills(workerCtx, service)
	}
	if !readOnly {
		go runDailyGiftExpiration(workerCtx, store.ExpireDailyCredits)
	}
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); taskruntime.Run(workerCtx, taskRuntime, cfg.Tasks.RestartInterval) }()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	log.Printf("drama-agent listening at http://%s/demo/", address)
	select {
	case <-signalCtx.Done():
	case err = <-done:
		if err != http.ErrServerClosed {
			log.Print(err)
		}
	}
	stopWorker()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err = host.Shutdown(shutdown); err != nil {
		log.Print("model shutdown deadline reached; unresolved requests will not be resent")
	}
	select {
	case <-workerDone:
	case <-shutdown.Done():
		log.Print("task worker shutdown deadline; PG evidence retained for recovery")
	}
	if err = server.Shutdown(shutdown); err != nil {
		log.Print("HTTP shutdown deadline reached")
	}
}
