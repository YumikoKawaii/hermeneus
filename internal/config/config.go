package config

type Config struct {
	ListenAddr string     `yaml:"listenAddr"`
	Server     CHServer   `yaml:"server"`
	StarRocks  StarRocks  `yaml:"starrocks"`
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
	MySQLDSN       string `yaml:"mysqlDSN"`
	StreamLoadHost string `yaml:"streamLoadHost"`
	StreamLoadUser string `yaml:"streamLoadUser"`
	StreamLoadPass string `yaml:"streamLoadPass"`
	Database       string `yaml:"database"`
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
