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
	// Handle container init process
	if len(os.Args) > 1 && os.Args[1] == "init" {
		if err := container.Run(); err != nil {
			log.Fatal(err)
		}

		return
	}

	root := &ffcli.Command{
		Name:       appName,
		ShortHelp:  "tinydock is a minimal implementation of container runtime",
		ShortUsage: "tinydock COMMAND",
		FlagSet:    flag.NewFlagSet(appName, flag.ExitOnError),
		Subcommands: []*ffcli.Command{
			newRunCmd(),
			newListCmd(),
			newStopCmd(),
			newRemoveCmd(),
			newLogsCmd(),
			newExecCmd(),
			newCommitCmd(),
		},
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

func newRunCmd() *ffcli.Command {
	runFlagSet := flag.NewFlagSet("run", flag.ExitOnError)

	interactive := runFlagSet.Bool("it", false, "Run container in interactive mode")
	autoRemove := runFlagSet.Bool("rm", false, "Automatically remove the container when it exits")
	detached := runFlagSet.Bool("d", false, "Run container in detached mode")

	cpuLimit := runFlagSet.Float64("c", 0, "CPU limit (e.g., 0.5 for 50% of one core)")
	memoryLimit := runFlagSet.String("m", "", "Memory limit (e.g., 100m)")

	var volumes volume.Volumes
	runFlagSet.Var(&volumes, "v", "Bind mount a volume (e.g., /host:/container)")

	var envs container.Envs
	runFlagSet.Var(&envs, "e", "Set environment variables")

	return &ffcli.Command{
		Name:       "run",
		ShortHelp:  "Create and run a new container",
		ShortUsage: "tinydock run (-it [-rm] | -d) [-c CPU] [-m MEMORY] [-v SRC:DST]... [-e KEY=VALUE]... COMMAND",
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

			return container.Init(*interactive, *autoRemove, *detached, *cpuLimit, *memoryLimit, volumes, args, envs)
		},
	}
}

func newListCmd() *ffcli.Command {
	listFlagSet := flag.NewFlagSet("ls", flag.ExitOnError)

	showAll := listFlagSet.Bool("a", false, "Show all containers (default shows running)")

	return &ffcli.Command{
		Name:       "ls",
		ShortUsage: "tinydock ls [-a]",
		ShortHelp:  "List containers",
		FlagSet:    listFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("'tinydock ls' accepts no arguments")
			}

			return container.List(*showAll)
		},
	}
}

func newStopCmd() *ffcli.Command {
	stopFlagSet := flag.NewFlagSet("stop", flag.ExitOnError)

	sig := stopFlagSet.String("s", "", "Signal to send to the container")

	return &ffcli.Command{
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
					continue
				}
				log.Println(id)
			}

			return nil
		},
	}
}

func newRemoveCmd() *ffcli.Command {
	removeFlagSet := flag.NewFlagSet("rm", flag.ExitOnError)

	force := removeFlagSet.Bool("f", false, "Force the removal of a running container")

	return &ffcli.Command{
		Name:       "rm",
		ShortUsage: "tinydock rm [-f] CONTAINER",
		ShortHelp:  "Remove one or more containers",
		FlagSet:    removeFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("'tinydock rm' requires at least 1 argument")
			}

			for _, id := range args {
				if err := container.Remove(id, *force); err != nil {
					log.Printf("Error removing container %s: %v", id, err)
					continue
				}
				log.Println(id)
			}

			return nil
		},
	}
}

func newLogsCmd() *ffcli.Command {
	logsFlagSet := flag.NewFlagSet("logs", flag.ExitOnError)

	follow := logsFlagSet.Bool("f", false, "Follow log output")

	return &ffcli.Command{
		Name:       "logs",
		ShortUsage: "tinydock logs [-f] CONTAINER",
		ShortHelp:  "Fetch the logs of a container",
		FlagSet:    logsFlagSet,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("'tinydock logs' requires exactly 1 argument")
			}

			return container.Logs(args[0], *follow)
		},
	}
}

func newExecCmd() *ffcli.Command {
	return &ffcli.Command{
		Name:       "exec",
		ShortUsage: "tinydock exec CONTAINER COMMAND [ARGS...]",
		ShortHelp:  "Execute a command in a running container",
		Exec: func(ctx context.Context, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("'tinydock exec' requires at least 2 arguments")
			}

			return container.Exec(args[0], args[1:])
		},
	}
}

func newCommitCmd() *ffcli.Command {
	return &ffcli.Command{
		Name:       "commit",
		ShortUsage: "tinydock commit CONTAINER NAME",
		ShortHelp:  "Create a new image from a container's changes",
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("'tinydock commit' requires exactly 2 arguments")
			}

			return container.Commit(args[0], args[1])
		},
	}
}
