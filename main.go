package main

import (
	"crypto/tls"
	"encoding/json"
	"os"
)

var GlobalConfig Config

type Config struct {
	Role                string `json:"role"`
	Secret              string `json:"secret"`
	TCPConnect          string `json:"tcpConnect"`
	UDPConnect          string `json:"udpConnect"`
	TCPListen           string `json:"tcpListen"`
	UDPListen           string `json:"udpListen"`
	CertificateLocation string `json:"certificateLocation"`
	KeyLocation         string `json:"keyLocation"`
	TLSConfig           tls.Config
}

// initial setup
func init() {
	// create config file path
	configPath := "config.json"

	// add path prefix if provided
	if len(os.Args) > 1 {
		configPath = os.Args[1] + configPath
	}

	// read config file
	bytes, err := os.ReadFile(configPath)
	if err != nil {
		panic(err)
	}

	// parse config file
	err = json.Unmarshal(bytes, &GlobalConfig)
	if err != nil {
		panic(err)
	}

	// load certificates
	certificate, err := tls.LoadX509KeyPair(GlobalConfig.CertificateLocation, GlobalConfig.KeyLocation)
	if err != nil {
		panic(err)
	}

	// update tls config
	GlobalConfig.TLSConfig.MinVersion = tls.VersionTLS13
	GlobalConfig.TLSConfig.Certificates = []tls.Certificate{certificate}
	GlobalConfig.TLSConfig.InsecureSkipVerify = true
}

func main() {
	if GlobalConfig.Role == "server" {
		s := Server{}
		s.Run()
	} else if GlobalConfig.Role == "client" {
		c := Client{}
		c.Run()
	} else {
		panic("invalid role: " + GlobalConfig.Role)
	}
}
