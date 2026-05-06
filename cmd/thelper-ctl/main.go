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
	storageProvider := flag.String("storage-provider", "sqlite", "storage provider")
	storageDSN := flag.String("storage-dsn", ".artifacts/dev/sqlite/t-helper.db", "storage DSN or sqlite path")
	configPath := flag.String("config", "config.json", "config file path")
	ignorePath := flag.String("ignore", ".t-helper.ignore", "ignore file path")
	reconfigure := flag.Bool("reconfigure", false, "import config and ignore rules")
	reload := flag.Bool("reload", false, "reload runtime config from database")
	restart := flag.String("restart", "", "restart one module")
	migrateDB := flag.Bool("migrate-db", false, "migrate active database to migration profile")
	flag.Parse()

	command := ctl.Command{
		Name:            "providers",
		StorageProvider: *storageProvider,
		StorageDSN:      *storageDSN,
		ConfigPath:      *configPath,
		IgnorePath:      *ignorePath,
		ModuleName:      *restart,
	}
	switch {
	case *reconfigure:
		command.Name = "reconfigure"
	case *reload:
		command.Name = "reload"
	case *restart != "":
		command.Name = "restart"
	case *migrateDB:
		command.Name = "migrate-db"
	case flag.NArg() > 0:
		command.Name = flag.Arg(0)
	}

	app := ctl.New(os.Stdout, storageproviders.MVPRegistry())
	if err := app.Run(context.Background(), command); err != nil {
		stdlog.Fatal(fmt.Errorf("thelper-ctl: %w", err))
	}
}
