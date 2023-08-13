package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/sync/syncmap"
)

var config Config

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

func createConnectionToClient() (*tls.Conn, error) {
	// connect to client
	connectionToClient, err := tls.Dial("tcp", config.TCPConnect, &config.TLSConfig)
	if err != nil {
		return nil, err
	}

	// initialize connection
	_, err = connectionToClient.Write([]byte(config.Secret))
	if err != nil {
		if connectionToClient != nil {
			connectionToClient.Close()
		}
		return nil, err
	}

	// read first packet from client
	buffer := make([]byte, 1024*8)
	readBytes, err := connectionToClient.Read(buffer)
	if err != nil {
		if connectionToClient != nil {
			connectionToClient.Close()
		}
		return nil, err
	}
	if string(buffer[:readBytes]) != "ok" {
		if connectionToClient != nil {
			connectionToClient.Close()
		}
		return nil, err
	}
	return connectionToClient, nil
}

func handleConnectionToClient(connectionToClient *tls.Conn) {
	// parse local service address
	localServiceAddress, err := net.ResolveUDPAddr("udp4", config.UDPConnect)
	if err != nil {
		fmt.Printf("failed to parse local service address %s\n%s\n", config.UDPConnect, err.Error())
		return
	}

	// create connection serivce
	connectionToLocalService, err := net.DialUDP("udp4", nil, localServiceAddress)
	if err != nil {
		fmt.Println(err)
		if connectionToLocalService != nil {
			connectionToLocalService.Close()
		}
		return
	}

	// close connections when done
	defer func() {
		if connectionToLocalService != nil {
			connectionToLocalService.Close()
		}
		if connectionToClient != nil {
			connectionToClient.Close()
		}
	}()

	// handle incoming packets from client
	go func() {
		d := time.Hour
		b := make([]byte, 1024*8)
		var n int
		var e error
		for {
			// set read deadline
			e = connectionToClient.SetReadDeadline(time.Now().Add(d))
			if e != nil {
				return
			}

			// read packet from client
			n, e = connectionToClient.Read(b)
			if e != nil {
				return
			}
			// write packet to local service
			_, e = connectionToLocalService.Write(b[:n])
			if e != nil {
				return
			}
		}
	}()

	// handle incoming packets from local service
	b := make([]byte, 1024*8)
	var n int
	var e error
	for {
		// read packet from local service
		n, e = connectionToLocalService.Read(b)
		if e != nil {
			return
		}
		// write packet to client
		_, e = connectionToClient.Write(b[:n])
		if e != nil {
			return
		}
	}
}

func init() {
	configPath := "config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1] + configPath
	}
	bytes, err := os.ReadFile(configPath)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		panic(err)
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateLocation, config.KeyLocation)
	if err != nil {
		panic(err)
	}
	config.TLSConfig.MinVersion = tls.VersionTLS12
	config.TLSConfig.Certificates = []tls.Certificate{certificate}
	config.TLSConfig.InsecureSkipVerify = true
	config.TLSConfig.CurvePreferences = []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256}
	config.TLSConfig.CipherSuites = []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA,
	}
}

func main() {
	if config.Role == "server" {
		// create master conncetion
		masterConnectionToClient, err := createConnectionToClient()
		for err != nil {
			time.Sleep(time.Second)
			masterConnectionToClient, err = createConnectionToClient()
		}
		fmt.Println("stablished master connection to client")

		b := make([]byte, 1024*8)
		var e error
		for {
			// read from master connection to client
			_, e = masterConnectionToClient.Read(b)
			if e != nil {
				masterConnectionToClient, err = createConnectionToClient()
				for err != nil {
					masterConnectionToClient, err = createConnectionToClient()
					time.Sleep(time.Second)
				}
			}

			// check for commands
			if b[0] == byte(0) { // create new connection to client
				go func() {
					connectionToClient, e := createConnectionToClient()
					if e != nil {
						return
					}
					handleConnectionToClient(connectionToClient)
				}()
			} else if b[0] == byte(1) {
				_, err = masterConnectionToClient.Write([]byte{2})
				if err != nil {
					if masterConnectionToClient != nil {
						masterConnectionToClient.Close()
					}
				}
			}
		}
	} else {
		pool := make(chan *net.Conn, 1024)
		userAddressToConnectionTable := syncmap.Map{}
		var masterConnectionToServer *net.Conn = nil

		go func() {
			var e error
			b := make([]byte, 1024*8)
			for {
				if masterConnectionToServer != nil {
					_, e = (*masterConnectionToServer).Read(b)
					if e != nil {
						if masterConnectionToServer != nil {
							(*masterConnectionToServer).Close()
							masterConnectionToServer = nil
						}
					}
					if b[0] == byte(1) {
						_, e = (*masterConnectionToServer).Write([]byte{2})
						if e != nil {
							if masterConnectionToServer != nil {
								(*masterConnectionToServer).Close()
								masterConnectionToServer = nil
							}
						}
					}
				}
			}
		}()

		go func() {
			// create local listener
			listenAddress, err := net.ResolveUDPAddr("udp4", config.UDPListen)
			if err != nil {
				panic(err)
			}
			localListener, err := net.ListenUDP("udp4", listenAddress)
			if err != nil {
				panic(err)
			}
			defer localListener.Close()
			fmt.Println("listening on " + config.UDPListen)

			// handle packets from users
			b := make([]byte, 1024*8)
			for {
				// read packet from user
				n, userAddress, e := localListener.ReadFromUDP(b)
				if e != nil {
					continue
				}

				// check if user has connection to server
				if conn, ok := userAddressToConnectionTable.Load(userAddress.String()); ok {
					_, e = conn.(net.Conn).Write(b[:n])
					if e != nil {
						conn.(net.Conn).Close()
						userAddressToConnectionTable.Delete(userAddress.String())
					}
				} else {
					go func(buff []byte) {
						if masterConnectionToServer == nil {
							return
						}
						_, e = (*masterConnectionToServer).Write([]byte{0})
						if e != nil {
							masterConnectionToServer = nil
							return
						}
						connectionToServer := <-pool
						userAddressToConnectionTable.Store(userAddress.String(), connectionToServer)
						go func(userAddr *net.UDPAddr) {
							defer func() {
								if connectionToServer != nil {
									(*connectionToServer).Close()
								}
								userAddressToConnectionTable.Delete(userAddress.String())
							}()
							buff := make([]byte, 1024*8)
							var num int
							var err error
							for {
								num, err = (*connectionToServer).Read(buff)
								if err != nil {
									return
								}
								_, err = localListener.WriteToUDP(buff[:num], userAddr)
								if err != nil {
									return
								}
							}
						}(userAddress)
						_, e = (*connectionToServer).Write(buff)
						if e != nil {
							if connectionToServer != nil {
								(*connectionToServer).Close()
							}
							userAddressToConnectionTable.Delete(userAddress.String())
						}
					}(b[:n])
				}
			}
		}()

		// listen for incoming connection from server
		listener, err := tls.Listen("tcp", config.TCPListen, &config.TLSConfig)
		if err != nil {
			panic(err)
		}

		// accept new connections from server
		b := make([]byte, 1024*8)
		for {
			connectionToServer, err := listener.Accept()
			if err != nil {
				fmt.Println(err)
				continue
			}

			// read secret from client
			n, err := connectionToServer.Read(b)
			if err != nil {
				fmt.Println(err)
				if connectionToServer != nil {
					connectionToServer.Close()
				}
				continue
			}

			// check if secret is valid
			if string(b[:n]) != config.Secret {
				connectionToServer.Close()
				continue
			}

			// send ok packet to server
			_, err = connectionToServer.Write([]byte("ok"))
			if err != nil {
				fmt.Println(err)
				if connectionToServer != nil {
					connectionToServer.Close()
				}
				continue
			}

			if masterConnectionToServer == nil {
				// use the first connection as the master connection
				masterConnectionToServer = &connectionToServer
				fmt.Println("master connection to server stablished")
			} else {
				// add stablished connection to the pool
				pool <- &connectionToServer
			}
		}
	}
}
