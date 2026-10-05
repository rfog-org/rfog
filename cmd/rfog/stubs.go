package main

import (
	"flag"

	"rfog/client"
	"rfog/data"
)

// runClient starts the terminal client. Flags: -theme, -tier, -name.
func runClient(args []string) error { return startClient(args, "") }

// runBots starts the client straight into a match against bots.
func runBots(args []string) error { return startClient(args, "bots") }

// runOnline starts the client straight into the online menu.
func runOnline(args []string) error { return startClient(args, "online") }

func startClient(args []string, start string) error {
	fs := flag.NewFlagSet("rfog", flag.ContinueOnError)
	theme := fs.String("theme", "", "theme name (fibre mono amber green gruvbox nord catppuccin)")
	tier := fs.String("tier", "", "render tier override (t0 t1 t2)")
	name := fs.String("name", "", "operator name")
	server := fs.String("server", "", "server address host:port (tls://host:port for TLS)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := data.Load()
	if err != nil {
		return err
	}
	set := client.LoadSettings()
	if *theme != "" {
		set.Theme = *theme
	}
	if *tier != "" {
		set.Tier = *tier
	}
	if *name != "" {
		set.Name = *name
	}
	nosave := *theme != "" || *tier != "" || *name != ""
	return client.Run(client.Options{Content: c, Settings: set, Env: client.EnvFromOS(), Start: start, Server: *server, NoSave: nosave})
}
