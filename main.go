package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
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

func createConnectionToClient() {
	// connect to client
	connectionToClient, err := tls.Dial("tcp", config.TCPConnect, &config.TLSConfig)
	if err != nil {
		fmt.Printf("failed to connect to client at %s\n%s\n", config.TCPConnect, err.Error())
		return
	}

	// initialize connection
	_, err = connectionToClient.Write([]byte(config.Secret))
	if err != nil {
		fmt.Printf("failed to send raw http request to client at %s\n%s\n", config.TCPConnect, err.Error())
		return
	}

	// read first packet from client
	buffer := make([]byte, 1024*8)
	readBytes, err := connectionToClient.Read(buffer)
	if err != nil {
		fmt.Printf("failed to read first packet from client\n%s\n", err.Error())
		return
	}
	if string(buffer[:readBytes]) != "ok" {
		fmt.Printf("did not receive ok packet from client")
		return
	}

	// parse local service address
	localServiceAddress, err := net.ResolveUDPAddr("udp4", config.UDPConnect)
	if err != nil {
		fmt.Printf("failed to parse local service address %s\n%s\n", config.UDPConnect, err.Error())
		return
	}

	// create connection serivce
	connectionToLocalService, err := net.DialUDP("udp4", nil, localServiceAddress)
	if err != nil {
		fmt.Printf("failed to open udp connection to %s\n%s\n", config.UDPConnect, err.Error())
		return
	}

	// handle incoming packets from client
	go func() {
		b := make([]byte, 1024*8)
		var n int
		var e error
		for {
			// read packet from client
			n, e = connectionToClient.Read(b)
			if e != nil {
				fmt.Printf("failed to read packet from client\n%s\n", e.Error())
				break
			}
			// write packet to local service
			_, e = connectionToLocalService.Write(b[:n])
			if e != nil {
				fmt.Printf("failed to write packet to local service%s\n%s\n", config.UDPConnect, e.Error())
				break
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
			fmt.Printf("failed to read packet from %s\n%s\n", config.UDPConnect, e.Error())
			break
		}
		// write packet to client
		_, e = connectionToClient.Write(b[:n])
		if e != nil {
			fmt.Printf("failed to write packet to %s\n%s\n", config.TCPConnect, e.Error())
			break
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
		masterConnectionToClient, err := tls.Dial("tcp", config.TCPConnect, &config.TLSConfig)
		if err != nil {
			panic(fmt.Sprintf("failed to connect to client at %s\n%s\n", config.TCPConnect, err.Error()))
		}

		// initialize connection
		_, err = masterConnectionToClient.Write([]byte(config.Secret))
		if err != nil {
			panic(fmt.Sprintf("failed to send raw http request to client at %s\n%s\n", config.TCPConnect, err.Error()))
		}

		// read first packet from client
		buffer := make([]byte, 1024*8)
		readBytes, err := masterConnectionToClient.Read(buffer)
		if err != nil {
			panic(fmt.Sprintf("failed to read first packet from client\n%s\n", err.Error()))
		}
		if string(buffer[:readBytes]) != "ok" {
			panic("did not receive ok packet from client")
		}
		fmt.Println("created master connection to client")

		b := make([]byte, 1024*8)
		var n int
		var e error
		for {
			// read from master connection to client
			n, e = masterConnectionToClient.Read(b)
			if e != nil {
				panic(e)
			}

			// check for commands
			if string(b[:n]) == "0" { // create new connection to client
				fmt.Println("creating new connection to client")
				go createConnectionToClient()
			}
		}
	} else {
		pool := make(chan *net.Conn, 1024)
		userAddressToConnectionTable := make(map[string]*net.Conn)
		var masterConnectionToServer *net.Conn = nil

		go func() {
			// create local listener
			listenAddress, err := net.ResolveUDPAddr("udp4", config.UDPListen)
			if err != nil {
				fmt.Printf("failed to parse udp listen address %s\n%s\n", config.UDPListen, err.Error())
			}
			localListener, err := net.ListenUDP("udp4", listenAddress)
			if err != nil {
				fmt.Printf("failed to listen on %s\n%s", config.UDPListen, err.Error())
			}
			defer localListener.Close()
			fmt.Println("listening on " + config.UDPListen)

			// handle packets from users
			b := make([]byte, 1024*8)
			for {
				n, userAddress, e := localListener.ReadFromUDP(b)
				if e != nil {
					fmt.Printf("failed to read packet from user\n%s\n", e.Error())
					continue
				}

				if userAddressToConnectionTable[userAddress.String()] != nil {
					_, e = (*userAddressToConnectionTable[userAddress.String()]).Write(b[:n])
					if e != nil {
						fmt.Printf("failed to write packet to server\n%s\n", e.Error())
					}
				} else {
					go func(buff []byte) {
						fmt.Println("waiting for connection from server")
						(*masterConnectionToServer).Write([]byte("0"))
						connectionToServer := <-pool
						fmt.Println("assigning connection to user")
						userAddressToConnectionTable[userAddress.String()] = connectionToServer
						go func(userAddr *net.UDPAddr) {
							buff := make([]byte, 1024*8)
							var num int
							var error error
							for {
								num, error = (*connectionToServer).Read(buff)
								if error != nil {
									fmt.Printf("failed to read packet from server\n%s\n", error.Error())
									break
								}
								_, error = localListener.WriteToUDP(buff[:num], userAddr)
								if error != nil {
									fmt.Printf("failed to write packet to user at %s\n%s\n", userAddr, error.Error())
									break
								}
							}
						}(userAddress)
						_, e = (*connectionToServer).Write(buff)
						if e != nil {
							fmt.Printf("failed to write packet to server\n%s\n", e.Error())
							return
						}
					}(b[:n])
				}
			}
		}()

		// listen for incoming connection from server
		listener, err := tls.Listen("tcp", config.TCPListen, &config.TLSConfig)
		if err != nil {
			panic(fmt.Sprintf("failed to create listener on %s,\n%s\n", config.TCPListen, err.Error()))
		}

		// accept new connections from server
		b := make([]byte, 1024*8)
		for {
			connectionToServer, err := listener.Accept()
			if err != nil {
				fmt.Printf("failed to accept new connection from server\n%s\n", err.Error())
				continue
			}

			// read secret from client
			n, err := connectionToServer.Read(b)
			if err != nil {
				fmt.Printf("failed to read secret from server\n%s\n", err.Error())
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
				fmt.Printf("failed to send ok packet to server\n%s\n", err.Error())
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
