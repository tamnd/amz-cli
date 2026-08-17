// Command amz is a delightful CLI for Amazon.com.
package main

import (
	"context"
	"errors"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/tamnd/amz-cli/cli"
)

func main() {
	root := cli.Root()
	err := fang.Execute(
		context.Background(),
		root,
		fang.WithVersion(cli.Version),
		fang.WithCommit(cli.Commit),
	)
	if err == nil {
		return
	}
	// fang has already printed the error. All that is left is the exit code,
	// which is the part of an error a script can read. Printing here as well put
	// every failure on the terminal twice, once styled by fang and once with an
	// "amz:" prefix, which reads like two things went wrong.
	var ee *cli.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.Code)
	}
	os.Exit(1)
}
