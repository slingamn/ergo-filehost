// Copyright (c) 2021 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package lib

import (
	"os"

	"github.com/slingamn/ergo-filehost/lib/custime"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Server struct {
		ListenAddress string `yaml:"listen-address"`
		TLS           *struct {
			Cert string `yaml:"cert"`
			Key  string `yaml:"key"`
		} `yaml:"tls"`
		Paths struct {
			Upload   string `yaml:"upload"`
			Files    string `yaml:"files"`
			FilesURL string `yaml:"files-url"`
		} `yaml:"paths"`
	} `yaml:"server"`

	Directory string `yaml:"directory"`

	Limits struct {
		Expiration  custime.Duration `yaml:"expiration"`
		MaxFileSize int64            `yaml:"max-file-size"`
	} `yaml:"limits"`

	Ergo struct {
		APIURL      string `yaml:"api-url"`
		BearerToken string `yaml:"bearer-token"`
	} `yaml:"ergo"`

	Logging string `yaml:"logging"`

	StripExifMetadata bool `yaml:"strip-exif-metadata"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
