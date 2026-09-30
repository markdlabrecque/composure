package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/markdlabrecque/composure/internal/config"
	"github.com/markdlabrecque/composure/internal/store"
)

func runConfig(args []string, stdout, stderr io.Writer) int {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	if verb != "validate" && verb != "export" {
		return configFailure(stderr, "", 2, fmt.Errorf("usage_error: unknown config subcommand %q", verb))
	}
	flags := flag.NewFlagSet("config "+verb, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", "", "configuration file")
	siteDir := flags.String("site", "", "site directory")
	out := flags.String("out", "", "output file")
	if err := flags.Parse(args[1:]); err != nil {
		return configFailure(stderr, verb, 2, fmt.Errorf("usage_error: %v", err))
	}
	if flags.NArg() != 0 {
		return configFailure(stderr, verb, 2, fmt.Errorf("usage_error: positional arguments are not supported"))
	}
	if verb == "validate" {
		if *file == "" || *siteDir != "" || *out != "" {
			return configFailure(stderr, verb, 2, fmt.Errorf("usage_error: validate requires --file FILE only"))
		}
		data, err := os.ReadFile(*file)
		if err != nil {
			return configFailure(stderr, verb, 1, fmt.Errorf("io_error: cannot read input file: %v", err))
		}
		if _, err := config.Validate(data); err != nil {
			var validation *config.ValidationError
			if errors.As(err, &validation) {
				return configFailure(stderr, verb, validation.ExitCode, validation)
			}
			return configFailure(stderr, verb, 3, err)
		}
		if _, err := fmt.Fprintln(stdout, "Valid Page configuration format v1."); err != nil {
			return configFailure(stderr, verb, 1, fmt.Errorf("io_error: %v", err))
		}
		return 0
	}
	if *siteDir == "" || *out == "" || *file != "" {
		return configFailure(stderr, verb, 2, fmt.Errorf("usage_error: export requires --site DIR and --out FILE only"))
	}
	databasePath := filepath.Join(*siteDir, "composure.db")
	if err := rejectProtectedOutput(*siteDir, *out); err != nil {
		return configFailure(stderr, verb, 1, fmt.Errorf("io_error: %v", err))
	}
	active, err := store.ReadActiveConfig(context.Background(), databasePath)
	if err != nil {
		return configFailure(stderr, verb, storeErrorCode(err), err)
	}
	document, err := config.Validate(active.Document)
	if err != nil {
		var validation *config.ValidationError
		if errors.As(err, &validation) {
			return configFailure(stderr, verb, validation.ExitCode, validation)
		}
		return configFailure(stderr, verb, 3, err)
	}
	canonical, err := config.Canonical(document)
	if err != nil {
		return configFailure(stderr, verb, 1, fmt.Errorf("io_error: %v", err))
	}
	if err := installFile(*out, canonical); err != nil {
		return configFailure(stderr, verb, 1, fmt.Errorf("io_error: %v", err))
	}
	if _, err := fmt.Fprintf(stdout, "Exported Page configuration format v1 to %s.\n", *out); err != nil {
		return configFailure(stderr, verb, 1, fmt.Errorf("io_error: %v", err))
	}
	return 0
}

func configFailure(stderr io.Writer, verb string, code int, err error) int {
	message := strings.ReplaceAll(strings.ReplaceAll(err.Error(), "\r", " "), "\n", " ")
	if verb == "" {
		fmt.Fprintf(stderr, "composure config: %s\n", message)
	} else {
		fmt.Fprintf(stderr, "composure config %s: %s\n", verb, message)
	}
	return code
}

func storeErrorCode(err error) int {
	var state *store.StateError
	if errors.As(err, &state) {
		return state.Code
	}
	return 1
}

func installFile(path string, data []byte) (err error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".composure-config-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(temporaryPath)
		}
	}()
	n, writeErr := temporary.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	installed = true
	return nil
}

func rejectProtectedOutput(siteDir, output string) error {
	protected := []string{
		filepath.Join(siteDir, "composure.db"),
		filepath.Join(siteDir, "composure.db-wal"),
		filepath.Join(siteDir, "composure.db-shm"),
	}
	outputPath, err := canonicalPath(output)
	if err != nil {
		return err
	}
	outputInfo, outputStatErr := os.Stat(output)
	for _, candidate := range protected {
		candidatePath, err := canonicalPath(candidate)
		if err != nil {
			return err
		}
		if outputPath == candidatePath {
			return fmt.Errorf("output target resolves to protected site database path %s", candidate)
		}
		candidateInfo, candidateStatErr := os.Stat(candidate)
		if outputStatErr == nil && candidateStatErr == nil && os.SameFile(outputInfo, candidateInfo) {
			return fmt.Errorf("output target aliases protected site database path %s", candidate)
		}
	}
	return nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	current := absolute
	var remainder []string
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Clean(filepath.Join(append([]string{resolved}, remainder...)...)), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if current == filepath.Dir(current) {
			return filepath.Clean(absolute), nil
		}
		remainder = append([]string{filepath.Base(current)}, remainder...)
		current = filepath.Dir(current)
	}
}
