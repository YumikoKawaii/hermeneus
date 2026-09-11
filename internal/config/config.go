package config

import (
	"os"
)

type Config struct {
	ListenAddr string    `yaml:"listenAddr"`
	Server     CHServer  `yaml:"server"`
	StarRocks  StarRocks `yaml:"starrocks"`
}

type CHServer struct {
	ServerName      string `yaml:"serverName"`
	VersionMajor    int    `yaml:"versionMajor"`
	VersionMinor    int    `yaml:"versionMinor"`
	VersionPatch    int    `yaml:"versionPatch"`
	ProtocolVersion int    `yaml:"protocolVersion"`
	Database        string `yaml:"database"`
}

type StarRocks struct {
	MySQLDSN string `yaml:"mysqlDSN"`
	Database string `yaml:"database"`
}

func Default() Config {
	return Config{
		ListenAddr: ":9000",
		Server: CHServer{
			ServerName:      "Hermeneus",
			VersionMajor:    24,
			VersionMinor:    3,
			VersionPatch:    1,
			ProtocolVersion: 54465,
			Database:        "default",
		},
	}
}

func FromEnv() Config {
	c := Default()
	if v := os.Getenv("HERMENEUS_LISTEN_ADDR"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("HERMENEUS_DATABASE"); v != "" {
		c.Server.Database = v
		c.StarRocks.Database = v
	}
	if v := os.Getenv("HERMENEUS_SR_MYSQL_DSN"); v != "" {
		c.StarRocks.MySQLDSN = v
	}
	return c
}
