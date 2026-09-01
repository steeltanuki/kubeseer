// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/steeltanuki/kubeseer/internal/purge"
)

var (
	buildVersion = "unknown"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "--version" {
		_, err := fmt.Fprintf(stdout, "kubeseer-purge version=%s commit=%s date=%s\n", valueOrUnknown(buildVersion), valueOrUnknown(buildCommit), valueOrUnknown(buildDate))
		return err
	}
	options := purge.Options{Timeout: 2 * time.Minute}
	flags := flag.NewFlagSet("kubeseer-purge", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.KubeconfigPath, "kubeconfig", "", "absolute kubeconfig path")
	flags.StringVar(&options.ContextName, "context", "", "explicit Kubernetes context")
	flags.StringVar(&options.ConfirmContext, "confirm-context", "", "repeat the exact context as destructive confirmation")
	flags.StringVar(&options.ConfirmServer, "confirm-server", "", "resolved API server URL as destructive confirmation")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "bounded purge timeout")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || flags.Arg(0) != purge.ConfirmationToken {
		return errors.New("usage: kubeseer-purge --kubeconfig /absolute/path --context CONTEXT --confirm-context CONTEXT --confirm-server SERVER purge-kubeseer-crds")
	}
	options.Confirmation = flags.Arg(0)
	if options.Timeout <= 0 {
		return errors.New("purge timeout must be positive")
	}

	restConfig, target, err := purge.ResolveTarget(options)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "PURGE_TARGET context=%s server=%s\n", target.Context, target.Server); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "PURGE_PLAN collections=kubeseers.kubeseer.io,kubeseeraccesspolicies.kubeseer.io order=instances-before-crds"); err != nil {
		return err
	}

	report, err := purge.Execute(context.Background(), restConfig, options, target)
	for _, result := range report.Results {
		if result.Error == "" {
			if _, printErr := fmt.Fprintf(stdout, "PURGE target=%s action=%s outcome=%s\n", result.Target, result.Action, result.Outcome); printErr != nil {
				return printErr
			}
			continue
		}
		if _, printErr := fmt.Fprintf(stdout, "PURGE target=%s action=%s outcome=%s error=%s\n", result.Target, result.Action, result.Outcome, result.Error); printErr != nil {
			return printErr
		}
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "PURGE_STATUS=passed")
	return err
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
