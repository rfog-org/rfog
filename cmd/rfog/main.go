// Command rfog is the game: client, server, bots, replay viewer, art tools.
package main

import (
	"fmt"
	"os"
)

// version is set at build time (make VERSION=v0.1.0 ...); "dev" otherwise.
var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	var err error
	switch cmd {
	case "", "play":
		err = runClient(args)
	case "serve":
		err = runServe(args)
	case "bots":
		err = runBots(args)
	case "online":
		err = runOnline(args)
	case "replay":
		err = runReplay(args)
	case "balance":
		err = runBalance(args)
	case "bench":
		err = runBench(args)
	case "status":
		err = runStatus(args)
	case "stats":
		err = runStats(args)
	case "asciify":
		err = runAsciify(args)
	case "version", "-v", "--version":
		fmt.Println("rfog", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: rfog [command]

  rfog                 client (flags: -server host:port -theme -tier -name)
  rfog online          client, straight to the online menu
  rfog serve           run a server (flags: -tcp :7777 -ssh :2222 -ws :8080 -db sqlite://rfog.db)
  rfog bots            offline match vs bots
  rfog version         print the version
  rfog replay <file>   print a replay as a text log
  rfog status          your ratings and matches (-json for status bars)
  rfog stats           server-wide counts per day, read from the store (-db -days)
  rfog balance         bot-vs-bot win rates (-n 20 -mode 2v2 -json)
  rfog bench           is it us or your terminal? (-n 100 -cells small)
  rfog asciify         art pipeline
`)
}
