// Command server runs kkrepo-go: a Go backend for a Go module proxy plus the
// embedded Bifrost-style console.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"kkrepo-go/internal/api"
	"kkrepo-go/internal/ldapauth"
	"kkrepo-go/internal/observability"
	"kkrepo-go/internal/oidc"
	"kkrepo-go/internal/security"
	"kkrepo-go/internal/service"
	"kkrepo-go/internal/signing"
	"kkrepo-go/internal/store"
	"kkrepo-go/web"
)

var version = "0.2.0"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export":
			runExport(os.Args[2:])
			return
		case "import":
			runImport(os.Args[2:])
			return
		case "help", "-h", "--help":
			usage()
			return
		case "server":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		}
	}
	addr := flag.String("addr", env("KKREPO_ADDR", ":8080"), "HTTP listen address")
	dataDir := flag.String("data", env("KKREPO_DATA", "./data"), "metadata and blob directory")
	adminUser := flag.String("admin-user", env("KKREPO_ADMIN_USER", "admin"), "console admin username")
	adminPassword := flag.String("admin-password", env("KKREPO_ADMIN_PASSWORD", "admin123"), "console admin password")
	sessionSecret := flag.String("session-secret", env("KKREPO_SESSION_SECRET", ""), "session HMAC secret")
	publicURL := flag.String("public-url", env("KKREPO_PUBLIC_URL", ""), "public base URL used in printed GOPROXY hints")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	st, err := store.New(*dataDir)
	if err != nil {
		logger.Error("open store", "error", err)
		os.Exit(1)
	}
	adminHash, err := security.HashPassword(*adminPassword)
	if err != nil {
		logger.Error("hash admin password", "error", err)
		os.Exit(1)
	}
	if err := st.EnsureAdmin(*adminUser, "管理员", adminHash); err != nil {
		logger.Error("seed admin user", "error", err)
		os.Exit(1)
	}
	protocol := service.New(st)
	aptSigner, err := signing.LoadOrCreateAPT(*dataDir)
	if err != nil {
		logger.Error("load APT signing key", "error", err)
		os.Exit(1)
	}
	alpineSigner, err := signing.LoadOrCreateAlpine(*dataDir)
	if err != nil {
		logger.Error("load Alpine signing key", "error", err)
		os.Exit(1)
	}
	protocol.SetSigningKeys(aptSigner, alpineSigner)
	auth := api.NewAuth(st, *sessionSecret, 12*time.Hour)
	auth.SetLDAP(ldapauth.New(ldapauth.Config{
		URL: env("KKREPO_LDAP_URL", ""), BaseDN: env("KKREPO_LDAP_BASE_DN", ""),
		BindDN: env("KKREPO_LDAP_BIND_DN", ""), BindPassword: env("KKREPO_LDAP_BIND_PASSWORD", ""),
		UserFilter:   env("KKREPO_LDAP_USER_FILTER", "(uid=%s)"),
		DefaultRoles: splitCSV(env("KKREPO_LDAP_DEFAULT_ROLES", "nx-developer")),
	}))
	oidcRedirect := env("KKREPO_OIDC_REDIRECT_URL", "")
	if oidcRedirect == "" {
		base := *publicURL
		if base == "" {
			base = "http://localhost" + *addr
		}
		oidcRedirect = strings.TrimRight(base, "/") + "/api/oidc/callback"
	}
	auth.SetOIDC(oidc.New(oidc.Config{
		Issuer: env("KKREPO_OIDC_ISSUER", ""), ClientID: env("KKREPO_OIDC_CLIENT_ID", ""),
		ClientSecret: env("KKREPO_OIDC_CLIENT_SECRET", ""), RedirectURL: oidcRedirect,
		Scopes:       env("KKREPO_OIDC_SCOPES", "openid profile email"),
		DefaultRoles: splitCSV(env("KKREPO_OIDC_DEFAULT_ROLES", "nx-developer")),
	}))
	server := api.New(api.Options{
		Store:     st,
		Service:   protocol,
		Auth:      auth,
		Metrics:   observability.New(1000),
		Assets:    web.Dist(),
		Version:   version,
		GoVersion: runtime.Version(),
		DataDir:   *dataDir,
		Logger:    logger,
		PublicURL: *publicURL,
	})

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		logger.Info("kkrepo-go started",
			"addr", *addr,
			"data", *dataDir,
			"console", "http://localhost"+*addr,
			"goproxy", "http://localhost"+*addr+"/repository/go-group",
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdown
	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func usage() {
	fmt.Fprintf(os.Stderr, `kkrepo-go %s

Usage:
  kkrepo-go server [flags]           start the HTTP server (default)
  kkrepo-go export -out backup.tgz   export a consistent backup archive
  kkrepo-go import -in backup.tgz -force
                                     restore/replace from an archive
  kkrepo-go import -in backup.tgz -mode merge
                                     merge new repositories only

Server flags:
  -addr, -data, -admin-user, -admin-password, -session-secret, -public-url
`, version)
}

func runExport(args []string) {
	flags := flag.NewFlagSet("export", flag.ExitOnError)
	dataDir := flags.String("data", env("KKREPO_DATA", "./data"), "metadata and blob directory")
	out := flags.String("out", "", "output archive path ('-' for stdout)")
	_ = flags.Parse(args)
	if *out == "" {
		fmt.Fprintln(os.Stderr, "export: -out is required")
		os.Exit(2)
	}
	st, err := store.New(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "export:", err)
		os.Exit(1)
	}
	write := func(w *os.File) error { return st.Export(w) }
	if *out == "-" {
		if err := st.Export(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "export:", err)
			os.Exit(1)
		}
		return
	}
	temp := *out + ".tmp"
	file, err := os.Create(temp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "export:", err)
		os.Exit(1)
	}
	if err := write(file); err != nil {
		file.Close()
		os.Remove(temp)
		fmt.Fprintln(os.Stderr, "export:", err)
		os.Exit(1)
	}
	if err := file.Close(); err != nil {
		os.Remove(temp)
		fmt.Fprintln(os.Stderr, "export:", err)
		os.Exit(1)
	}
	if err := os.Rename(temp, *out); err != nil {
		fmt.Fprintln(os.Stderr, "export:", err)
		os.Exit(1)
	}
	fmt.Printf("exported %s\n", *out)
}

func runImport(args []string) {
	flags := flag.NewFlagSet("import", flag.ExitOnError)
	dataDir := flags.String("data", env("KKREPO_DATA", "./data"), "metadata and blob directory")
	in := flags.String("in", "", "input archive path ('-' for stdin)")
	mode := flags.String("mode", "replace", "import mode: replace or merge")
	force := flags.Bool("force", false, "required for replace mode")
	_ = flags.Parse(args)
	if *in == "" {
		fmt.Fprintln(os.Stderr, "import: -in is required")
		os.Exit(2)
	}
	st, err := store.New(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		os.Exit(1)
	}
	reader := os.Stdin
	if *in != "-" {
		file, err := os.Open(*in)
		if err != nil {
			fmt.Fprintln(os.Stderr, "import:", err)
			os.Exit(1)
		}
		defer file.Close()
		reader = file
	}
	result, err := st.ImportArchive(reader, store.ImportOptions{Mode: *mode, Force: *force})
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		os.Exit(1)
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
