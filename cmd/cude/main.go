package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/MARCAAAAARRON/cude/internal/agent"
	"github.com/MARCAAAAARRON/cude/internal/config"
	"github.com/MARCAAAAARRON/cude/internal/project"
	"github.com/MARCAAAAARRON/cude/internal/router"
	"github.com/MARCAAAAARRON/cude/internal/session"
	"github.com/MARCAAAAARRON/cude/internal/tools"
	"github.com/MARCAAAAARRON/cude/internal/tui"
)

// Build-time variables — injected via -ldflags by Makefile / GoReleaser.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	var (
		modelFlag   = flag.String("model", "", "Override default model")
		configFlag  = flag.String("config", "", "Path to explicit config file")
		versionFlag = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *versionFlag {
		fmt.Printf("cude %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// 1. Load config
	cfg, err := config.Load(*configFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cude: config error: %v\n", err)
		os.Exit(1)
	}

	modelName := cfg.DefaultModel
	if *modelFlag != "" {
		modelName = *modelFlag
	}

	// 2. Initialize Model Router
	r, err := router.New(&cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cude: router error: %v\n", err)
		os.Exit(1)
	}
	defer r.Close()

	be, err := r.GetBackend(modelName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cude: %v\n", err)
		os.Exit(1)
	}

	// 3. Detect Project Workspace
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cude: getwd: %v\n", err)
		os.Exit(1)
	}
	proj, err := project.Detect(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cude: project detect: %v\n", err)
		os.Exit(1)
	}

	// 4. Initialize Tools
	registry := tools.NewRegistry()
	registry.Register(tools.NewFileReadTool(proj.Root))
	registry.Register(tools.NewFileWriteTool(proj.Root))
	registry.Register(tools.NewShellTool(proj.Root))
	registry.Register(tools.NewProjectSearchTool(proj.Root))
	registry.Register(tools.NewListFilesTool(proj.Root))

	// 5. Initialize Agent Core
	core := agent.New(cfg.Agent, be, registry)

	// 6. Initialize Session Manager
	sm := session.NewManager(proj.Root)

	// 7. Launch TUI
	ctx := context.Background()
	if err := tui.Run(ctx, core, r, sm); err != nil {
		fmt.Fprintf(os.Stderr, "cude: tui error: %v\n", err)
		os.Exit(1)
	}
}
