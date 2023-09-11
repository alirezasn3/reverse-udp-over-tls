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
	for {
		connectionToClient, e := s.CreateConnection()
		if e != nil {
			fmt.Printf("[%s] failed to create new connection\n", e.Error())
			time.Sleep(time.Second)
			continue
		}

		// handle connection to client on new go routine
		go s.HandleConnection(connectionToClient)

		time.Sleep(time.Millisecond * 100)
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
