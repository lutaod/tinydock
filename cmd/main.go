package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/peterbourgon/ff/v3/ffcli"

	"github.com/lutaod/tinydock/internal/container"
	"github.com/lutaod/tinydock/internal/volume"
)

const appName = "tinydock"

func main() {
	// Handle "init" argument, which signals that current process should act as the init process
	// (PID 1) of container
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := container.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Definitions related to run command
	runFlagSet := flag.NewFlagSet("run", flag.ExitOnError)

	interactive := runFlagSet.Bool("it", false, "Run container in interactive mode")

	memoryLimit := runFlagSet.String("m", "", "Memory limit (e.g., 100m)")

	cpuLimit := runFlagSet.Float64("c", 0, "CPU limit (e.g., 0.5 for 50% of one core)")

	var volumes volume.Volumes
	runFlagSet.Var(&volumes, "v", "Bind mount a volume (e.g., /host:/container)")

	runCmd := &ffcli.Command{
		Name:       "run",
		ShortHelp:  "Create and run a new container",
		ShortUsage: "tinydock run [-it] [-m MEMORY] [-c CPU] [-v SRC:DST]... COMMAND",
		FlagSet:    runFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("'tinydock run' requires at least 1 argument")
			}
			return container.Create(*interactive, *memoryLimit, *cpuLimit, volumes, args)
		},
	}

	// Definitions related to root command
	rootFlagSet := flag.NewFlagSet(appName, flag.ExitOnError)

	root := &ffcli.Command{
		Name:        appName,
		ShortHelp:   "tinydock is a minimal implementation of container runtime",
		ShortUsage:  "tinydock COMMAND",
		FlagSet:     rootFlagSet,
		Subcommands: []*ffcli.Command{runCmd},
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return flag.ErrHelp
			}
			return fmt.Errorf("'%s' is not a tinydock command.\nSee 'tinydock --help'", args[0])
		},
	}

	if err := root.ParseAndRun(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
