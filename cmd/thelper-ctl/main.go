package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"os"

	"github.com/artBass-rip/t-helper/internal/app/ctl"
	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
)

func main() {
	flag.Parse()
	command := "providers"
	if flag.NArg() > 0 {
		command = flag.Arg(0)
	}

	app := ctl.New(os.Stdout, storageproviders.MVPRegistry())
	if err := app.RunCommand(context.Background(), command); err != nil {
		stdlog.Fatal(fmt.Errorf("thelper-ctl: %w", err))
	}
}
