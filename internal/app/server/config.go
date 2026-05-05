package server

type Config struct {
	ListenAddress   string
	Mode            string
	StorageProvider string
	StorageDSN      string
	LogLevel        string
	MigrateOnly     bool
}

func DefaultConfig() Config {
	return Config{
		ListenAddress:   "127.0.0.1:8080",
		Mode:            "local",
		StorageProvider: "sqlite",
		StorageDSN:      ".artifacts/dev/sqlite/t-helper.db",
		LogLevel:        "info",
	}
}
