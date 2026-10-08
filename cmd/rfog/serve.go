package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"rfog/data"
	"rfog/server"
	"rfog/server/gateway"
	"rfog/server/store"
)

// runServe starts a server. Every flag has a RFOG_* environment fallback
// so a systemd unit or container needs no arguments.
func runServe(args []string) error {
	fs := flag.NewFlagSet("rfog serve", flag.ContinueOnError)
	tcp := fs.String("tcp", env("RFOG_LISTEN_TCP", ":7777"), "TCP listen address for native clients (empty to disable)")
	sshAddr := fs.String("ssh", env("RFOG_LISTEN_SSH", ":2222"), "SSH listen address for zero-install play (empty to disable)")
	wsAddr := fs.String("ws", env("RFOG_LISTEN_WS", ":8080"), "HTTP listen address for the browser client (empty to disable)")
	wsOrigins := fs.String("ws-origins", env("RFOG_WS_ORIGINS", ""), "comma-separated extra Origin hosts allowed to open web sessions (same-origin always allowed)")
	db := fs.String("db", env("RFOG_DB", "sqlite://rfog.db"), "store URL: sqlite://path, sqlite::memory: or postgres://user:pass@host/db")
	motd := fs.String("motd", env("RFOG_MOTD", ""), "message of the day")
	hostKey := fs.String("hostkey", env("RFOG_HOSTKEY", ".ssh/rfog_host_ed25519"), "SSH host key path (generated if missing)")
	tlsCert := fs.String("tls-cert", env("RFOG_TLS_CERT", ""), "TLS certificate for the TCP listener (optional)")
	tlsKey := fs.String("tls-key", env("RFOG_TLS_KEY", ""), "TLS key for the TCP listener")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := data.Load()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	st, err := store.Open(ctx, *db)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()
	logger := log.New(os.Stderr, "rfog ", log.LstdFlags)
	srv := server.New(c, st, server.Config{MOTD: *motd, Logger: logger})
	go func() {
		if err := srv.Run(ctx); err != nil {
			logger.Printf("server: %v", err)
		}
	}()
	logger.Printf("store %s", *db)
	var origins []string
	for _, o := range strings.Split(*wsOrigins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	return gateway.Run(ctx, srv, c, gateway.Options{TCP: *tcp, SSH: *sshAddr, WS: *wsAddr, WSOrigins: origins, TLSCert: *tlsCert, TLSKey: *tlsKey, HostKey: *hostKey, Logger: logger})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// runStats prints the server's activity per day from its store: accounts,
// guests, online matches, players. Live counts (online now, peak) are the
// "stats" lines in the server's log.
func runStats(args []string) error {
	fs := flag.NewFlagSet("rfog stats", flag.ContinueOnError)
	db := fs.String("db", env("RFOG_DB", "sqlite://rfog.db"), "store URL, as for rfog serve")
	days := fs.Int("days", 14, "how many days back")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	st, err := store.Open(ctx, *db)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()
	a, err := st.Activity(ctx, time.Now().AddDate(0, 0, -(*days-1)))
	if err != nil {
		return err
	}
	fmt.Printf("accounts %d · seen today %d · this week %d · this month %d\n\n", a.Accounts, a.Seen[0], a.Seen[1], a.Seen[2])
	fmt.Printf("%-10s  %8s  %6s  %7s  %8s  %6s  %5s  %7s\n", "day (UTC)", "accounts", "guests", "matches", "finished", "humans", "rated", "players")
	for _, d := range a.Days {
		fmt.Printf("%-10s  %8d  %6d  %7d  %8d  %6d  %5d  %7d\n", d.Date, d.Accounts, d.Guests, d.Matches, d.Finished, d.Humans, d.Rated, d.Players)
	}
	fmt.Println("\nmatches: online only (vs bots in the web app or the terminal never reach the server).")
	fmt.Println("humans: matches with two or more people. players: distinct people in that day's matches.")
	fmt.Println("live counts: grep \"stats online\" in the server log.")
	return nil
}
