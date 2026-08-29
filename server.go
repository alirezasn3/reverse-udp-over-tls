package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	HasActiveConnectionToClient atomic.Bool
}

func (s *Server) Run() {
	s.HasActiveConnectionToClient.Store(false)

	for range time.NewTicker(time.Second).C {
		if s.HasActiveConnectionToClient.Load() {
			continue
		}

		connectionToClient, e := s.CreateConnection()
		if e != nil {
			log.Printf("[%s] failed to create new tcp connection to %s\n", e.Error(), GlobalConfig.TCPConnect)
		} else {
			log.Printf("created new tcp connection from %s to client at %s\n", connectionToClient.LocalAddr().String(), GlobalConfig.TCPConnect)

			s.HasActiveConnectionToClient.Store(true)

			// handle connection to client on new go routine
			go s.HandleConnection(connectionToClient)
		}
	}
}

func (s *Server) CreateConnection() (*tls.Conn, error) {
	// connect to client
	c, e := tls.DialWithDialer(
		&net.Dialer{Timeout: time.Second * 1, KeepAliveConfig: net.KeepAliveConfig{Idle: time.Second * 10, Interval: time.Second * 10, Count: 1}},
		"tcp",
		GlobalConfig.TCPConnect,
		&tls.Config{
			InsecureSkipVerify: true,
			VerifyConnection: func(cs tls.ConnectionState) error {
				certs := cs.PeerCertificates
				if len(certs) == 0 {
					return fmt.Errorf("no certificates provided by client")
				}
				// Extract the Organization field injected on the client
				orgs := certs[0].Subject.Organization
				if len(orgs) == 0 || orgs[0] != GlobalConfig.Secret {
					return fmt.Errorf("unauthorized: invalid proxy secret token")
				}
				return nil // Validation passed
			},
		},
	)
	if e != nil {
		return nil, e
	}
	return c, nil
}

func (s *Server) HandleConnection(connectionToClient *tls.Conn) {
	defer func() {
		s.HasActiveConnectionToClient.Store(false)
	}()

	// close connection to client when done
	defer connectionToClient.Close()

	// parse local service address
	localServiceAddress, err := net.ResolveUDPAddr("udp4", GlobalConfig.UDPConnect)
	if err != nil {
		log.Printf("failed to parse local service address %s\n%s\n", GlobalConfig.UDPConnect, err.Error())
		return
	}

	// create connection to serivce
	connectionToLocalService, err := net.DialUDP("udp4", nil, localServiceAddress)
	if err != nil {
		log.Println(err)
		return
	}
	defer connectionToLocalService.Close()

	// timeout
	d := time.Minute

	var shouldClose atomic.Bool
	shouldClose.Store(false)

	var wg sync.WaitGroup

	// handle incoming packets from client
	wg.Add(1)
	go func() {
		defer wg.Done()

		b := make([]byte, 1500)
		var n int
		var e error
		for {
			if shouldClose.Load() {
				return
			}

			// set read deadline
			e = connectionToClient.SetReadDeadline(time.Now().Add(d))
			if e != nil {
				if !shouldClose.Load() {
					shouldClose.Store(true)
					log.Println("failed to set read deadline for tcp connection to client")
				}
				connectionToLocalService.Close()
				return
			}

			// read packet from client
			n, e = connectionToClient.Read(b)
			if e != nil {
				if !shouldClose.Load() {
					shouldClose.Store(true)
					log.Println("failed to read from tcp connection to client")
				}
				connectionToLocalService.Close()
				return
			}

			// write packet to local service
			_, e = connectionToLocalService.Write(b[:n])
			if e != nil {
				if !shouldClose.Load() {
					shouldClose.Store(true)
					log.Println("failed to write packet to local udp service")
				}
				connectionToClient.Close()
				return
			}
		}
	}()

	// handle incoming packets from local service
	wg.Add(1)
	go func() {
		defer wg.Done()

		b := make([]byte, 1500)
		var n int
		var e error
		for {
			if shouldClose.Load() {
				return
			}

			// set read deadline
			e = connectionToLocalService.SetReadDeadline(time.Now().Add(d))
			if e != nil {
				if !shouldClose.Load() {
					shouldClose.Store(true)
					log.Println("failed to set read deadline for udp connection to local service")
				}
				connectionToClient.Close()
				return
			}

			// read packet from local service
			n, e = connectionToLocalService.Read(b)
			if e != nil {
				if !shouldClose.Load() {
					shouldClose.Store(true)
					log.Println("failed to read packet from local udp service")
				}
				connectionToClient.Close()
				return
			}

			// write packet to client
			_, e = connectionToClient.Write(b[:n])
			if e != nil {
				if !shouldClose.Load() {
					shouldClose.Store(true)
					log.Println("failed to write packet to tcp connection to client")
				}
				connectionToLocalService.Close()
				return
			}
		}
	}()

	wg.Wait()
}
