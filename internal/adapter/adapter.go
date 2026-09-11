package adapter

import "github.com/yumikokawaii/hermeneus/internal/extractor"

type Reader interface {
	Read(*extractor.Statement) (string, error)
}

type Writer interface {
	Write(target string, records []extractor.Record) error
}
