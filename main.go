package main

import (
	"flag"
	"fmt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"

	"github.com/usabarashi/terraform-provider-synclayer/internal/provider"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var (
		debug       bool
		showVersion bool
	)

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.BoolVar(&showVersion, "version", false, "print the provider version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		return
	}

	plugin.Serve(&plugin.ServeOpts{
		Debug:        debug,
		ProviderAddr: "registry.terraform.io/usabarashi/synclayer",
		ProviderFunc: provider.New,
	})
}
