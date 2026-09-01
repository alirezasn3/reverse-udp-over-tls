package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log"
	"math/big"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func generateMemoryCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ephemeral-server", Organization: []string{GlobalConfig.Secret}},
		NotBefore:    time.Now().Add(-1 * time.Minute),
		NotAfter:     time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}

func addrPortToString(a netip.AddrPort) string {
	ip := a.Addr().As4()
	p := a.Port()
	b := make([]byte, 0, 6)
	for i := range ip {
		b = append(b, ip[i])
	}
	return string(append(b, byte(p), byte(p>>8)))
}

type Client struct {
	HasActiveConnectionToServer atomic.Bool
}

func (c *Client) Run() {
	c.HasActiveConnectionToServer.Store(false)

	// parse local udp listener address
	listenAddress, err := net.ResolveUDPAddr("udp4", GlobalConfig.UDPListen)
	if err != nil {
		panic(err)
	}

	// generate ssl certificate
	cert, err := generateMemoryCert()
	if err != nil {
		log.Println(err)
		return
	}

	// create tcp listener to listen for connections from the server
	listener, err := tls.Listen("tcp", GlobalConfig.TCPListen, &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		log.Println(err)
		return
	}
	log.Printf("listening on %s for tcp connections from the server...\n", GlobalConfig.TCPListen)

	// accept new tcp connection from server
	for {
		connectionToServer, e := listener.Accept()
		if e != nil {
			log.Printf("[%s] failed to accept new connection\n", e.Error())
			continue
		}
		if c.HasActiveConnectionToServer.Load() {
			// close connection
			connectionToServer.Close()
			log.Println("closed new tcp connection from server, already have active connection")
		} else {
			log.Printf("accepted new tcp connection from server at %s\n", connectionToServer.RemoteAddr().String())
			localListener, err := net.ListenUDP("udp4", listenAddress)
			if err != nil {
				panic(err)
			}
			log.Printf("listening on %s for udp packets from local service\n", GlobalConfig.UDPListen)

			c.HasActiveConnectionToServer.Store(true)

			var wg sync.WaitGroup

			// read deadline for both connections
			d := time.Second * 10

			var a atomic.Pointer[net.UDPAddr]

			// handle incoming tcp packets from the server
			wg.Go(func() {
				b := make([]byte, 1500)
				var n int
				var e error
				var ta *net.UDPAddr

				for {
					// set read deadline
					e = connectionToServer.SetReadDeadline(time.Now().Add(d))
					if e != nil {
						log.Println("failed to set read deadline for tcp connection to server")
						connectionToServer.Close()
						localListener.Close()
						break
					}

					// read packet from server
					n, e = connectionToServer.Read(b)
					if e != nil {
						log.Println("failed to read from tcp connection to server")
						connectionToServer.Close()
						localListener.Close()
						break
					}

					// write packet to local service
					ta = a.Load()
					if ta == nil {
						continue
					}
					_, e = localListener.WriteToUDP(b[:n], a.Load())
					if e != nil {
						log.Println("failed to write packet to local udp service")
						connectionToServer.Close()
						localListener.Close()
						break
					}
				}
				log.Println("exiting tcp handler go routine")
			})

			// handle incoming udp packets from local service
			wg.Go(func() {
				b := make([]byte, 1500)
				var n int
				var e error
				var ta, ta2 *net.UDPAddr

				for {
					// set read deadline
					e = localListener.SetReadDeadline(time.Now().Add(d))
					if e != nil {
						log.Println("failed to set read deadline for udp connection to local service")
						connectionToServer.Close()
						localListener.Close()
						break
					}

					// read udp packet from local client
					n, ta, e = localListener.ReadFromUDP(b)
					if e != nil {
						log.Println("failed to read packet from local udp service")
						connectionToServer.Close()
						localListener.Close()
						break
					}

					// update udp clinet address
					if ta != nil {
						ta2 = a.Load()
						if ta2 == nil || ta2.Port != ta.Port || !ta2.IP.Equal(ta.IP) {
							a.Store(ta)
						}
					}

					// write packet to server
					_, e = connectionToServer.Write(b[:n])
					if e != nil {
						log.Println("failed to write packet to tcp connection to server")
						connectionToServer.Close()
						localListener.Close()
						break
					}
				}

				log.Println("exiting udp handler go routine")
			})

			wg.Wait()

			c.HasActiveConnectionToServer.Store(false)

			localListener.Close()

			// run post down command
			if GlobalConfig.ClientPostDown != "" {
				cmdParts := strings.Split(GlobalConfig.ClientPostDown, " ")
				cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
				cmd.Env = append(
					os.Environ(),
					GlobalConfig.ClientPostDownEnvironment...,
				)
				o, e := cmd.CombinedOutput()
				if e != nil {
					log.Printf("failed to run post down script: %s: %s\n", e.Error(), o)
				} else {
					log.Println(string(o))
				}
			}

			log.Printf("listening on %s for tcp connections from the server...\n", GlobalConfig.TCPListen)
		}
	}
}
