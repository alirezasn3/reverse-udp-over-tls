package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"
)

type Server struct {
	MasterConnection           *tls.Conn
	ActiveConnections          sync.Map
	CleaningUpMasterConnection bool
	ClientAddress              string
}

func (s *Server) Run() {
	// keep master connection alive and reconnect if needed
	go func() {
		var e error
		for {
			if s.MasterConnection == nil {
				fmt.Printf("creating master connection to %s...\n", s.ClientAddress)
				for s.MasterConnection == nil {
					s.MasterConnection, e = s.CreateConnection()
					if e != nil {
						fmt.Printf("[%s] failed to create new connection to %s\n", e.Error(), s.ClientAddress)
						time.Sleep(time.Second)
					}
				}
				fmt.Printf("stablished master connection to %s\n", s.ClientAddress)

				// update read deadline
				e = s.MasterConnection.SetReadDeadline(time.Now().Add(time.Second * 3))
				if e != nil {
					fmt.Printf("[%s] failed to set read deadline, cleaning up...\n", e.Error())
					s.CleanUpMasterConnection()
				}
			}
			time.Sleep(time.Millisecond * 100)
		}
	}()

	// initialize loop vars
	d := time.Second * 3
	b := make([]byte, 1)
	var e error
	var n int

	// read from master connection
	for {
		// check if master connection exists
		if s.MasterConnection == nil {
			time.Sleep(time.Millisecond * 100)
			continue
		}

		// read packet
		n, e = s.MasterConnection.Read(b)
		if e != nil {
			fmt.Printf("[%s] failed to read from master connection, cleaning up...\n", e.Error())
			s.CleanUpMasterConnection()
			continue
		}
		if n == 0 {
			fmt.Println("read 0 bytes from master connection, cleaning up...")
			s.CleanUpMasterConnection()
			continue
		}

		if int(b[0]) == 1 { // respond to ping
			_, e = s.MasterConnection.Write([]byte{1})
			if e != nil {
				fmt.Printf("[%s] failed to respond to ping message, cleaning up...\n", e.Error())
				s.CleanUpMasterConnection()
				continue
			}
		} else if int(b[0]) == 2 { // create new connection to client
			go func() {
				connectionToClient, e := s.CreateConnection()
				if e != nil {
					fmt.Printf("[%s] failed to create new connection\n", e.Error())
					return
				}

				// handle connection to client
				s.HandleConnection(connectionToClient)
			}()
		}

		// update read deadline
		e = s.MasterConnection.SetReadDeadline(time.Now().Add(d))
		if e != nil {
			fmt.Printf("[%s] failed to set read deadline, cleaning up...\n", e.Error())
			s.CleanUpMasterConnection()
		}
	}
}

func (s *Server) CreateConnection() (*tls.Conn, error) {
	// connect to client
	c, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second * 5}, "tcp", s.ClientAddress, &GlobalConfig.TLSConfig)
	if e != nil {
		return nil, e
	}

	// store created connection in active connections map
	s.ActiveConnections.Store(c.LocalAddr().String(), c)

	return c, nil
}

func (s *Server) CleanUpMasterConnection() {
	// check if master connection exists and not already closing
	if s.CleaningUpMasterConnection || s.MasterConnection == nil {
		return
	}

	s.CleaningUpMasterConnection = true
	s.MasterConnection.Close()
	s.MasterConnection = nil
	s.CleaningUpMasterConnection = false

	fmt.Printf("master connection to %s closed\n", s.ClientAddress)
}

func (s *Server) HandleConnection(connectionToClient *tls.Conn) {
	// close connection to client when done
	defer connectionToClient.Close()

	// parse local service address
	localServiceAddress, err := net.ResolveUDPAddr("udp4", GlobalConfig.UDPConnect)
	if err != nil {
		fmt.Printf("failed to parse local service address %s\n%s\n", GlobalConfig.UDPConnect, err.Error())
		return
	}

	// create connection serivce
	connectionToLocalService, err := net.DialUDP("udp4", nil, localServiceAddress)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer connectionToLocalService.Close()

	// timeout
	d := time.Hour

	// handle incoming packets from client
	go func() {
		b := make([]byte, 1500)
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
	b := make([]byte, 1500)
	var n int
	var e error
	for {
		// set read deadline
		e = connectionToLocalService.SetReadDeadline(time.Now().Add(d))
		if e != nil {
			return
		}

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
