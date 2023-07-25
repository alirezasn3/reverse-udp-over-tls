package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
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

func ByteSliceToUint16(byteSlice []byte) uint16 {
	return uint16(byteSlice[0]) | uint16(byteSlice[1])<<8
}

func Uint16ToByteSlice(n uint16) []byte {
	temp := []byte{}
	return append(temp, byte(n), byte(n>>8))
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
		idToConnectionTable := make(map[uint16]*net.UDPConn)

		// connect to server
		connectionToClient, err := tls.Dial("tcp", config.TCPConnect, &config.TLSConfig)
		if err != nil {
			panic(fmt.Sprintf("failed to connect to client at %s\n%s\n", config.TCPConnect, err.Error()))
		}

		// initialize connection
		_, err = connectionToClient.Write([]byte(fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\nContent-Type: text/plain\r\n\r\n%s", config.TCPConnect, len(config.Secret), config.Secret)))
		if err != nil {
			panic(fmt.Sprintf("failed to send raw http request to client at %s\n%s\n", config.TCPConnect, err.Error()))
		}

		buffer := make([]byte, 1024*8)
		var n int
		var id uint16
		localServiceAddress, err := net.ResolveUDPAddr("udp4", config.UDPConnect)
		if err != nil {
			panic(fmt.Sprintf("failed to parse local service address %s\n%s\n", config.UDPConnect, err.Error()))
		}

		// read first packet from client
		n, err = connectionToClient.Read(buffer)
		if err != nil {
			panic(fmt.Sprintf("failed to read first packet from client\n%s\n", err.Error()))
		}
		if string(buffer[:n]) != "ok" {
			panic("did not receive ok packet from client")
		}

		for {
			// read incoming packets
			n, err = connectionToClient.Read(buffer)
			if err != nil {
				panic(fmt.Sprintf("failed to read packet from client\n%s\n", err.Error()))
			}

			// parse packet
			id = ByteSliceToUint16(buffer[0:2])

			// check if connection to local service exists
			if conn, ok := idToConnectionTable[id]; ok {
				_, err = conn.Write(buffer[2:n])
				if err != nil {
					panic(fmt.Sprintf("failed to write packet to local service\n%s\n", err.Error()))
				}
			} else {
				// create connection to local service
				fmt.Printf("received first packet from new user with id %d\n", id)
				idToConnectionTable[id], err = net.DialUDP("udp4", nil, localServiceAddress)
				if err != nil {
					panic(fmt.Sprintf("failed to open udp connection to %s for user with id %d\n%s\n", config.UDPConnect, id, err.Error()))
				}

				// write packet to local service
				_, err = idToConnectionTable[id].Write(buffer[2:n])
				if err != nil {
					panic(fmt.Sprintf("failed to write the first packet from client with id %d to %s\n%s\n", id, config.UDPConnect, err.Error()))
				}

				// handle incoming packets from local service
				go func(conn *net.UDPConn) {
					buffer := make([]byte, 1024*8)
					var n int
					var err error
					for {
						n, err = conn.Read(buffer)
						if err != nil {
							panic(fmt.Sprintf("failed to read packet from %s\n%s\n", config.UDPConnect, err.Error()))
						}

						_, err = connectionToClient.Write(append(Uint16ToByteSlice(id), buffer[:n]...))
						if err != nil {
							panic(fmt.Sprintf("failed to write packet with to %s\n%s\n", config.TCPConnect, err.Error()))
						}
					}
				}(idToConnectionTable[id])
			}
		}
	} else {
		if err := http.ListenAndServeTLS(config.TCPListen, config.CertificateLocation, config.KeyLocation,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					fmt.Printf("failed to read request body\n%s\n", err.Error())
					w.WriteHeader(400)
					return
				}
				if string(body) == config.Secret {
					fmt.Printf("received new tunnel request from %s\n", r.RemoteAddr)

					userAddressToIDTable := make(map[string]uint16)
					idToUserAddressTable := make(map[uint16]*net.UDPAddr)

					// hijack underlying connection
					connectionToServer, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						fmt.Printf("failed to hijack connection\n%s\n", err.Error())
						return
					}
					defer connectionToServer.Close()

					// send ok packet to server
					_, err = connectionToServer.Write([]byte("ok"))
					if err != nil {
						fmt.Printf("failed to send ok packet to server\n%s\n", err.Error())
						return
					}

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

					var wg sync.WaitGroup

					// handle packets from users
					wg.Add(1)
					go func() {
						buffer := make([]byte, 1024*8)
						var n int
						var ok bool
						var id uint16
						var userAddress *net.UDPAddr
						var err error
						for {
							n, userAddress, err = localListener.ReadFromUDP(buffer)
							if err != nil {
								fmt.Printf("failed to read packet from local listener\n%s\n", err.Error())
								wg.Done()
								wg.Done()
							}
							if id, ok = userAddressToIDTable[userAddress.String()]; !ok {
								id = uint16(len(userAddressToIDTable))
								userAddressToIDTable[userAddress.String()] = id
								idToUserAddressTable[id] = userAddress
							}
							_, err = connectionToServer.Write(append(Uint16ToByteSlice(id), buffer[:n]...))
							if err != nil {
								fmt.Printf("failed to write packet to server\n%s\n", err.Error())
								wg.Done()
								wg.Done()
							}
						}
					}()

					// handle packets from server
					wg.Add(1)
					go func() {
						buffer := make([]byte, 1024*8)
						var n int
						var ok bool
						var userAddress *net.UDPAddr
						for {
							n, err = connectionToServer.Read(buffer)
							if err != nil {
								fmt.Printf("failed to read packet from server\n%s\n", err.Error())
								wg.Done()
								wg.Done()
							}
							if userAddress, ok = idToUserAddressTable[ByteSliceToUint16(buffer[0:2])]; ok {
								_, err = localListener.WriteToUDP(buffer[2:n], userAddress)
								if err != nil {
									fmt.Printf("failed to write packet to user at %s with id %d\n%s\n", userAddress, ByteSliceToUint16(buffer[0:2]), err.Error())
								}
							} else {
								fmt.Printf("no user address found for id %d\n", ByteSliceToUint16(buffer[0:2]))
							}
						}
					}()

					wg.Wait()
				} else {
					w.WriteHeader(200)
					w.Write([]byte("Hello World!"))
				}
			}),
		); err != nil {
			panic(err)
		}
	}
}
