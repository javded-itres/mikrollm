package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/javded-itres/mikrollm/internal/app"
)

var version = "dev"

func main() {
	listen := flag.String("listen", env("MIKROLLM_LISTEN", ":4000"), "listen address")
	dataDir := flag.String("data", env("MIKROLLM_DATA", "./data"), "data directory")
	adminPass := flag.String("admin-password", os.Getenv("ADMIN_PASSWORD"), "set/reset admin password")
	resetPass := flag.Bool("admin-password-reset", os.Getenv("ADMIN_PASSWORD_RESET") == "1", "overwrite stored admin password")
	flag.Parse()

	application, err := app.New(app.Config{
		Listen:        *listen,
		DataDir:       *dataDir,
		AdminPassword: *adminPass,
		ResetPassword: *resetPass,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	srv := &http.Server{
		Addr:              *listen,
		Handler:           application.Handler,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("mikrollm %s listening on %s (data %s)", version, *listen, *dataDir)
	log.Fatal(srv.ListenAndServe())
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
