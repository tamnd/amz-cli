// Command amz is a delightful CLI for Amazon.com.
package main

import (
	"context"
	"errors"
	"fmt"
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
	var ee *cli.ExitError
	if errors.As(err, &ee) {
		if ee.Err != nil {
			fmt.Fprintln(os.Stderr, "amz:", ee.Err)
		}
		os.Exit(ee.Code)
	}
	fmt.Fprintln(os.Stderr, "amz:", err)
	os.Exit(1)
}
