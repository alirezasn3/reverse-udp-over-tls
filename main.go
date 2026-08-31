package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	goSystemd "github.com/alirezasn3/go-systemd"
)

var GlobalConfig Config
var path string

type Config struct {
	Role           string `json:"role"`
	TCPConnect     string `json:"tcpConnect"`
	TCPListen      string `json:"tcpListen"`
	UDPConnect     string `json:"udpConnect"`
	UDPListen      string `json:"udpListen"`
	Secret         string `json:"secret"`
	ClientPostDown string `json:"clientPostDown"`
	ServerPostDown string `json:"serverPostDown"`
}

// initial setup
func init() {
	execPath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	path = filepath.Dir(execPath)

	// read config file
	bytes, err := os.ReadFile(filepath.Join(path, "config.json"))
	if err != nil {
		panic(err)
	}

	// parse config file
	err = json.Unmarshal(bytes, &GlobalConfig)
	if err != nil {
		panic(err)
	}

	// check for install and uninstall commands
	if runtime.GOOS == "linux" {
		if slices.Contains(os.Args, "--install") {
			execPath, err := os.Executable()
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			err = goSystemd.CreateService(&goSystemd.Service{Name: "reverse-udp-over-tls", ExecStart: execPath, Restart: "on-failure", RestartSec: "5s"})
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			} else {
				fmt.Println("reverse-udp-over-tls service created")
				os.Exit(0)
			}
		} else if slices.Contains(os.Args, "--uninstall") {
			err := goSystemd.DeleteService("reverse-udp-over-tls")
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			} else {
				fmt.Println("reverse-udp-over-tls service deleted")
				os.Exit(0)
			}
		}
	}
}

func main() {
	switch GlobalConfig.Role {
	case "server":
		s := Server{}
		s.Run()
	case "client":
		c := Client{}
		c.Run()
	default:
		panic("invalid role: " + GlobalConfig.Role)
	}
}
