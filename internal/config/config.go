package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr string    `yaml:"listenAddr"`
	Sink       string    `yaml:"sink"`
	Server     CHServer  `yaml:"server"`
	StarRocks  StarRocks `yaml:"starrocks"`
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
	Host      string `yaml:"host"`
	QueryPort int    `yaml:"queryPort"`
	HTTPPort  int    `yaml:"httpPort"`
	User      string `yaml:"user"`
	Password  string `yaml:"password"`
	Database  string `yaml:"database"`
	WriteMode string `yaml:"writeMode"`
}

type Kafka struct {
	Brokers     []string          `yaml:"brokers"`
	TopicPrefix string            `yaml:"topicPrefix"`
	Topics      map[string]string `yaml:"topics"`
}

func Default() Config {
	return Config{
		ListenAddr: ":9000",
		Sink:       "starrocks",
		Server: CHServer{
			ServerName:      "Hermeneus",
			VersionMajor:    24,
			VersionMinor:    3,
			VersionPatch:    1,
			ProtocolVersion: 54465,
			Database:        "default",
		},
		StarRocks: StarRocks{
			QueryPort: 9030,
			HTTPPort:  8030,
			User:      "root",
			WriteMode: "stream_load",
		},
		Kafka: Kafka{
			TopicPrefix: "hermeneus.",
			Topics: map[string]string{
				"otel_logs":         "hermeneus.otel.logs",
				"otel_traces":       "hermeneus.otel.traces",
				"profiling_stacks":  "hermeneus.profiling.stacks",
				"profiling_samples": "hermeneus.profiling.samples",
			},
		},
	}
}

func FromEnv() Config {
	c := Default()
	if v := os.Getenv("HERMENEUS_LISTEN_ADDR"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("HERMENEUS_SINK"); v != "" {
		c.Sink = v
	}
	if v := os.Getenv("HERMENEUS_DATABASE"); v != "" {
		c.Server.Database = v
		c.StarRocks.Database = v
	}
	if v := os.Getenv("HERMENEUS_SR_HOST"); v != "" {
		c.StarRocks.Host = v
	}
	if v := os.Getenv("HERMENEUS_SR_QUERY_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.StarRocks.QueryPort = n
		}
	}
	if v := os.Getenv("HERMENEUS_SR_HTTP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.StarRocks.HTTPPort = n
		}
	}
	if v := os.Getenv("HERMENEUS_SR_USER"); v != "" {
		c.StarRocks.User = v
	}
	if v := os.Getenv("HERMENEUS_SR_PASSWORD"); v != "" {
		c.StarRocks.Password = v
	}
	if v := os.Getenv("HERMENEUS_SR_WRITE_MODE"); v != "" {
		c.StarRocks.WriteMode = v
	}
	if v := os.Getenv("HERMENEUS_KAFKA_BROKERS"); v != "" {
		c.Kafka.Brokers = strings.Split(v, ",")
	}
	return c
}
