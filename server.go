package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"
)

type Server struct {
	ClientAddress        string
	TotalDownload        uint64
	TotalUpload          uint64
	AttemptedConnections uint64
	AcceptedConnections  uint64
	ActiveConnections    uint64
	CurrentDownload      uint64
	CurrentUpload        uint64
	LatestConnection     int64
	Status               string
	D                    uint64 // to help calculate current donwload
	U                    uint64 // to help calculate current upload
}

func (s *Server) Run() {
	s.Status = "down"
	lastLog := time.Now()
	for {
		connectionToClient, e := s.CreateConnection()
		if e != nil {
			if e != io.EOF && !errors.Is(e, syscall.ECONNRESET) && time.Since(lastLog).Milliseconds() > 1000 {
				fmt.Printf("[%s] failed to create new connection to %s\n", e.Error(), s.ClientAddress)
				if s.Status == "up" {
					s.Status = "down"
				}
			} else {
				if s.Status == "down" {
					s.Status = "up"
				}
			}
		} else {
			fmt.Printf("created new connection to client to %s\n", s.ClientAddress)

			s.LatestConnection = time.Now().Unix()

			// handle connection to client on new go routine
			go s.HandleConnection(connectionToClient)

			if s.Status == "down" {
				s.Status = "up"
			}
		}
		time.Sleep(time.Millisecond * 200)
	}
}

func (s *Server) CreateConnection() (*tls.Conn, error) {
	s.AttemptedConnections++
	// connect to client
	c, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second * 1}, "tcp", s.ClientAddress, &GlobalConfig.TLSConfig)
	if e != nil {
		return nil, e
	}
	s.AcceptedConnections++
	s.ActiveConnections++
	return c, nil
}

func (s *Server) HandleConnection(connectionToClient *tls.Conn) {
	// close connection to client when done
	defer func() {
		s.ActiveConnections--
		connectionToClient.Close()
	}()

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
			s.TotalUpload += uint64(n)

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
		s.TotalDownload += uint64(n)

		// write packet to client
		_, e = connectionToClient.Write(b[:n])
		if e != nil {
			return
		}
	}
}
