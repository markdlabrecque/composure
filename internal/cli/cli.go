// Package cli owns command dispatch, flag parsing and exit codes.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/markdlabrecque/composure/internal/config"
	"github.com/markdlabrecque/composure/internal/site"
	"github.com/markdlabrecque/composure/internal/web"
)

// Version can be set at build time; local builds identify themselves as dev.
var Version = "dev"

func Run(args []string, stdout, stderr io.Writer) int {
	command := ""
	if len(args) > 1 {
		command = args[1]
	}
	fail := func(code int, err error) int {
		message := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
		fmt.Fprintf(stderr, "composure %s: %s\n", command, message)
		return code
	}
	if command == "version" {
		if len(args) != 2 {
			return fail(2, fmt.Errorf("version takes no arguments"))
		}
		if _, err := fmt.Fprintf(stdout, "composure %s\nschema version %d\nconfiguration format version %d\n", Version, site.SchemaVersion, config.FormatVersion); err != nil {
			return fail(1, err)
		}
		return 0
	}
	if command == "config" {
		return runConfig(args[2:], stdout, stderr)
	}
	if command != "init" && command != "serve" {
		return fail(2, fmt.Errorf("usage: composure <init|serve|version> [flags]"))
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("site", "", "site directory")
	var example, apply *bool
	var address *string
	var configFile *string
	if command == "init" {
		example = flags.Bool("example", false, "add one published example Page")
		apply = flags.Bool("apply", false, "apply the initialization plan")
		configFile = flags.String("config", "", "Page configuration file")
	} else {
		address = flags.String("addr", "127.0.0.1:8080", "loopback listen address")
	}
	if err := flags.Parse(args[2:]); err != nil {
		return fail(2, err)
	}
	if *dir == "" || flags.NArg() != 0 {
		return fail(2, fmt.Errorf("--site DIR is required; positional arguments are not supported"))
	}
	ctx := context.Background()
	if command == "init" {
		selected := config.Default
		if *configFile != "" {
			var err error
			selected, err = os.ReadFile(*configFile)
			if err != nil {
				return fail(1, fmt.Errorf("io_error: cannot read configuration file: %v", err))
			}
			if _, err = config.Validate(selected); err != nil {
				var validation *config.ValidationError
				if errors.As(err, &validation) {
					return fail(validation.ExitCode, validation)
				}
				return fail(3, err)
			}
		}
		if err := site.InitWithConfig(ctx, *dir, selected, *example, *apply, stdout, time.Now); err != nil {
			return fail(exitCode(err), err)
		}
		return 0
	}
	resolved, err := web.ResolveLoopback(ctx, *address)
	if err != nil {
		return fail(2, err)
	}
	repository, err := site.Open(ctx, *dir)
	if err != nil {
		return fail(exitCode(err), err)
	}
	defer repository.Close()
	listener, err := net.Listen("tcp", resolved)
	if err != nil {
		return fail(1, err)
	}
	defer listener.Close()
	if _, err = fmt.Fprintf(stdout, "listening on http://%s\n", listener.Addr()); err != nil {
		return fail(1, err)
	}
	if err = web.Server(repository, listener).Serve(listener); err != nil {
		return fail(1, err)
	}
	return 0
}
func exitCode(err error) int {
	var siteError *site.Error
	if errors.As(err, &siteError) {
		return siteError.Code
	}
	return 1
}
