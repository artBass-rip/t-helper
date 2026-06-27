package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/artBass-rip/t-helper/internal/toolchain"
)

func main() {
	dir := flag.String("dir", "", "directory where tflint and trivy are installed")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "-dir is required")
		os.Exit(2)
	}
	releases, err := toolchain.DefaultReleases(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := (toolchain.Installer{Dir: *dir, Releases: releases}).Install(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, release := range releases {
		fmt.Printf("installed %s %s in %s\n", release.Name, release.Version, *dir)
	}
}
