// ledgerforge-mcp exposes a LedgerForge deployment to MCP clients over stdio.
// Standard output is reserved for MCP protocol messages; operational logs are
// written to standard error.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/devaccuracy/ledgerforge"
	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/mcpserver"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "./ledgerforge.json", "path to LedgerForge configuration")
	allowWrites := flag.Bool("allow-write", false, "enable MCP tools that change ledger state")
	showVersion := flag.Bool("version", false, "print the server version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version)
		return
	}

	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.LUTC)

	if err := config.InitConfig(*configPath); err != nil {
		log.Fatalf("load configuration: %v", err)
	}
	configuration, err := config.Fetch()
	if err != nil {
		log.Fatalf("read configuration: %v", err)
	}

	datasource, err := database.NewDataSource(configuration)
	if err != nil {
		log.Fatalf("connect datasource: %v", err)
	}
	defer closeDatasource(datasource)

	service, err := ledgerforge.NewLedgerForge(datasource)
	if err != nil {
		log.Fatalf("initialize LedgerForge: %v", err)
	}
	defer func() {
		if err := service.Close(); err != nil {
			log.Printf("close LedgerForge: %v", err)
		}
	}()

	server := mcpserver.New(service, mcpserver.Options{
		Version:     version,
		AllowWrites: *allowWrites,
	})
	if *allowWrites {
		log.Print("LedgerForge MCP write tools are enabled")
	} else {
		log.Print("LedgerForge MCP is running in read-only mode; use --allow-write to enable state-changing tools")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Protocol().Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatalf("run MCP server: %v", err)
	}
}

func closeDatasource(datasource database.IDataSource) {
	closer, ok := datasource.(interface{ Close() error })
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		log.Printf("close datasource: %v", err)
	}
}
