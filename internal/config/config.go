package config

import (
	"os"
	"strings"
)

type Config struct {
	ListenAddr string    `yaml:"listenAddr"`
	Server     CHServer  `yaml:"server"`
	StarRocks  StarRocks `yaml:"starrocks"`
	Sink       string    `yaml:"sink"`
	Kafka      Kafka     `yaml:"kafka"`
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

type Kafka struct {
	Brokers     []string `yaml:"brokers"`
	TopicPrefix string   `yaml:"topicPrefix"`
	ClientID    string   `yaml:"clientID"`
}

const (
	SinkStreamLoad = "streamload"
	SinkKafka      = "kafka"
)

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
		Sink: SinkStreamLoad,
		Kafka: Kafka{
			TopicPrefix: "hermeneus.",
			ClientID:    "hermeneus",
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
	if v := os.Getenv("HERMENEUS_SR_STREAM_LOAD_HOST"); v != "" {
		c.StarRocks.StreamLoadHost = v
	}
	if v := os.Getenv("HERMENEUS_SR_STREAM_LOAD_USER"); v != "" {
		c.StarRocks.StreamLoadUser = v
	}
	if v := os.Getenv("HERMENEUS_SR_STREAM_LOAD_PASS"); v != "" {
		c.StarRocks.StreamLoadPass = v
	}
	if v := os.Getenv("HERMENEUS_SINK"); v != "" {
		c.Sink = strings.ToLower(v)
	}
	if v := os.Getenv("HERMENEUS_KAFKA_BROKERS"); v != "" {
		c.Kafka.Brokers = strings.Split(v, ",")
	}
	if v := os.Getenv("HERMENEUS_KAFKA_TOPIC_PREFIX"); v != "" {
		c.Kafka.TopicPrefix = v
	}
	if v := os.Getenv("HERMENEUS_KAFKA_CLIENT_ID"); v != "" {
		c.Kafka.ClientID = v
	}
	return c
}
