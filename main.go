package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var GlobalConfig Config
var servers []*Server
var totalDwonload uint64 = 0
var totalUpload uint64 = 0
var CurrentDownload uint64 = 0
var CurrentUpload uint64 = 0

type Config struct {
	Role                string   `json:"role"`
	TCPConnect          []string `json:"tcpConnect"`
	UDPConnect          string   `json:"udpConnect"`
	TCPListen           string   `json:"tcpListen"`
	UDPListen           string   `json:"udpListen"`
	MonitorAddress      string   `json:"monitorAddress"`
	CertificateLocation string   `json:"certificateLocation"`
	KeyLocation         string   `json:"keyLocation"`
	TemplatesLocation   string   `json:"templatesLocation"`
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
		var wg sync.WaitGroup
		for _, clientAddress := range GlobalConfig.TCPConnect {
			wg.Add(1)
			go func(a string) {
				s := Server{ClientAddress: a}
				servers = append(servers, &s)
				s.Run()
			}(clientAddress)
		}
		wg.Add(1)
		go func() {
			for range time.NewTicker(time.Second).C {
				totalDwonload = 0
				totalUpload = 0
				CurrentDownload = 0
				CurrentUpload = 0
				for _, s := range servers {
					s.CurrentDownload = s.TotalDownload - s.D
					s.CurrentUpload = s.TotalUpload - s.U
					s.D = s.TotalDownload
					s.U = s.TotalUpload
					totalDwonload += s.TotalDownload
					totalUpload += s.TotalUpload
					CurrentDownload += s.CurrentDownload
					CurrentUpload += s.CurrentUpload
				}
			}
		}()
		wg.Add(1)
		go func() {
			router := gin.Default()
			router.LoadHTMLGlob(GlobalConfig.TemplatesLocation)
			router.GET("/", func(c *gin.Context) {
				c.HTML(http.StatusOK, "index.html", gin.H{
					"servers":             servers,
					"tcpConnect":          GlobalConfig.TCPConnect,
					"udpConnect":          GlobalConfig.UDPConnect,
					"certificateLocation": GlobalConfig.CertificateLocation,
					"keyLocation":         GlobalConfig.KeyLocation,
					"currentDownload":     CurrentDownload,
					"currentUpload":       CurrentUpload,
					"totalDownload":       totalDwonload,
					"totalUpload":         totalUpload,
				})
			})
			router.GET("/ws", func(ctx *gin.Context) {
				upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
				conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
				if err != nil {
					http.Error(ctx.Writer, "Could not open websocket connection", http.StatusBadRequest)
					fmt.Println(err)
				}
				for {
					time.Sleep(time.Second)
					conn.WriteJSON(map[string]interface{}{
						"servers":         servers,
						"currentDownload": CurrentDownload,
						"currentUpload":   CurrentUpload,
						"totalDownload":   totalDwonload,
						"totalUpload":     totalUpload,
					})
				}
			})
			router.Run(GlobalConfig.MonitorAddress)
		}()
		wg.Wait()
	} else if GlobalConfig.Role == "client" {
		c := Client{}
		c.Run()
	} else {
		panic("invalid role: " + GlobalConfig.Role)
	}
}
