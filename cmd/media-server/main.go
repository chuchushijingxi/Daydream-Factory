// Command media-server runs FFmpeg/ffprobe/libwebp for the backend over the
// internal Compose network, so the Go backend image carries no media tools.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/mediatools"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:8790", "HTTP bind address on the internal network")
	healthcheck := flag.String("healthcheck", "", "probe this URL's health path and exit (container health check)")
	flag.Parse()
	if *healthcheck != "" {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(*healthcheck + mediatools.HealthPath)
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	// 工具从 PATH 解析（镜像中为 /opt/media/bin），超时沿用后端原有的默认值。
	tools := mediatools.Local{Settings: appconfig.Defaults().Media}
	server := &http.Server{
		Addr:              *listen,
		Handler:           mediatools.Handler(tools),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("media service listening at http://%s", *listen)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
