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

	autoRemove := runFlagSet.Bool("rm", false, "Automatically remove the container when it exits")

	detached := runFlagSet.Bool("d", false, "Run container in detached mode")

	name := runFlagSet.String("n", "", "Assign a name to container")

	cpuLimit := runFlagSet.Float64("c", 0, "CPU limit (e.g., 0.5 for 50% of one core)")

	memoryLimit := runFlagSet.String("m", "", "Memory limit (e.g., 100m)")

	var volumes volume.Volumes
	runFlagSet.Var(&volumes, "v", "Bind mount a volume (e.g., /host:/container)")

	var envs container.Envs
	runFlagSet.Var(&envs, "e", "Set environment variables")

	runCmd := &ffcli.Command{
		Name:       "run",
		ShortHelp:  "Create and run a new container",
		ShortUsage: "tinydock run (-it [-rm] | -d) [-n NAME] [-c CPU]  [-m MEMORY] [-v SRC:DST]... [-e KEY=VALUE]... COMMAND",
		FlagSet:    runFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("'tinydock run' requires at least 1 argument")
			}

			if *interactive && *detached {
				return fmt.Errorf("detached container cannot be interactive")
			}

			if !*interactive && *autoRemove {
				return fmt.Errorf("autoremove only works for interactive containers")
			}

			return container.Create(
				*interactive,
				*autoRemove,
				*detached,
				*name,
				*cpuLimit,
				*memoryLimit,
				volumes,
				args,
				envs,
			)
		},
	}

	// Definitions related to ls command
	lsFlagSet := flag.NewFlagSet("ls", flag.ExitOnError)

	showAll := lsFlagSet.Bool("a", false, "Show all containers (default shows just running)")

	lsCmd := &ffcli.Command{
		Name:       "ls",
		ShortUsage: "tinydock ls [-a]",
		ShortHelp:  "List containers",
		FlagSet:    lsFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			return container.List(*showAll)
		},
	}

	// Definitions related to stop command
	stopFlagSet := flag.NewFlagSet("stop", flag.ExitOnError)

	sig := stopFlagSet.String("s", "", "Signal to send to the container")

	stopCmd := &ffcli.Command{
		Name:       "stop",
		ShortUsage: "tinydock stop [-s SIGNAL] CONTAINER",
		ShortHelp:  "Stop one or more containers",
		FlagSet:    stopFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("'tinydock stop' requires at least 1 argument")
			}

			for _, id := range args {
				if err := container.Stop(id, *sig); err != nil {
					log.Printf("Error stopping container %s: %v", id, err)
				} else {
					log.Println(id)
				}
			}

			return nil
		},
	}

	// Definitions related to rm command
	rmFlagSet := flag.NewFlagSet("rm", flag.ExitOnError)

	force := rmFlagSet.Bool("f", false, "Force the removal of a running container")

	rmCmd := &ffcli.Command{
		Name:       "rm",
		ShortUsage: "tinydock rm [-f] CONTAINER",
		ShortHelp:  "Remove one or more containers",
		FlagSet:    rmFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("'tinydock rm' requires at least 1 argument")
			}

			for _, id := range args {
				if err := container.Remove(id, *force); err != nil {
					log.Printf("Error removing container %s: %v", id, err)
				} else {
					log.Println(id)
				}
			}

			return nil
		},
	}

	// Definitions related to logs command
	logsFlagSet := flag.NewFlagSet("logs", flag.ExitOnError)

	follow := logsFlagSet.Bool("f", false, "Follow log output")

	logsCmd := &ffcli.Command{
		Name:       "logs",
		ShortUsage: "tinydock logs [-f] CONTAINER",
		ShortHelp:  "Fetch the logs of a container",
		FlagSet:    logsFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'tinydock logs' exactly 1 argument")
			}

			return container.Logs(args[0], *follow)
		},
	}

	// Definitions related to root command
	rootFlagSet := flag.NewFlagSet(appName, flag.ExitOnError)

	root := &ffcli.Command{
		Name:        appName,
		ShortHelp:   "tinydock is a minimal implementation of container runtime",
		ShortUsage:  "tinydock COMMAND",
		FlagSet:     rootFlagSet,
		Subcommands: []*ffcli.Command{runCmd, lsCmd, stopCmd, rmCmd, logsCmd},
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
