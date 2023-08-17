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
}

func (s *Server) Run() {
	// keep master connection alive and reconnect if needed
	go func() {
		var e error
		for {
			if s.MasterConnection == nil {
				fmt.Println("creating master connection to client...")
				for s.MasterConnection == nil {
					s.MasterConnection, e = s.CreateConnection()
					if e != nil {
						fmt.Printf("[%s]\nfailed to crteate new connection\n", e.Error())
						time.Sleep(time.Second)
					}
				}
				fmt.Println("stablished master connection to client")
			}
			time.Sleep(time.Millisecond * 500)
		}
	}()

	// initialize loop vars
	d := time.Second * 5
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
			fmt.Printf("[%s]\nfailed to read from master connection, cleaning up...\n", e.Error())
			s.CleanUpMasterConnection()
			continue
		}
		if n == 0 {
			fmt.Println("read 0 bytes from master connection, cleaning up...")
			s.CleanUpMasterConnection()
			continue
		}

		// check for commands
		if b[0] == byte(0) {
			// create new connection to client
			go func() {
				connectionToClient, e := s.CreateConnection()
				if e != nil {
					fmt.Printf("[%s]\nfailed to create new connection\n", e.Error())
					return
				}

				// handle connection to client
				s.HandleConnection(connectionToClient)
			}()
		}

		// update read deadline
		e = s.MasterConnection.SetReadDeadline(time.Now().Add(d))
		if e != nil {
			fmt.Printf("[%s]\nfailed to set read deadline, cleaning up...\n", e.Error())
			s.CleanUpMasterConnection()
		}
	}
}

func (s *Server) CreateConnection() (*tls.Conn, error) {
	// connect to client
	c, e := tls.Dial("tcp", GlobalConfig.TCPConnect, &GlobalConfig.TLSConfig)
	if e != nil {
		if c != nil {
			c.Close()
		}
		return nil, e
	}

	// initialize connection
	_, e = c.Write([]byte(GlobalConfig.Secret))
	if e != nil {
		c.Close()
		return nil, e
	}

	// set read deadline for the new connectoin
	e = c.SetReadDeadline(time.Now().Add(time.Second * 3))
	if e != nil {
		c.Close()
		return nil, e
	}

	// read first packet from client
	buffer := make([]byte, len(GlobalConfig.Secret))
	readBytes, e := c.Read(buffer)
	if e != nil {
		if c != nil {
			c.Close()
		}
		return nil, e
	}
	if string(buffer[:readBytes]) != GlobalConfig.Secret {
		if c != nil {
			c.Close()
		}
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
}

func (s *Server) HandleConnection(connectionToClient *tls.Conn) {
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

	// timeout
	d := time.Hour * 12

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
