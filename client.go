package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
)

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
	ConnectionPool               chan net.Conn
	UserAddressToConnectionTable map[string]net.Conn
	CleaningUpMasterConnection   bool
	WaitingForConnection         bool
}

func (c *Client) Run() {
	c.WaitingForConnection = false

	// initialize connection pool
	c.ConnectionPool = make(chan net.Conn, 1024)

	// initialize connections table
	c.UserAddressToConnectionTable = make(map[string]net.Conn)

	// listen for new connections from server
	go func() {
		listener, err := tls.Listen("tcp", GlobalConfig.TCPListen, &GlobalConfig.TLSConfig)
		if err != nil {
			panic(err)
		}

		// accept new connections from server
		for {
			connectionToServer, e := listener.Accept()
			if e != nil {
				fmt.Printf("[%s] failed to accept new connection\n", e.Error())
				continue
			}
			if c.WaitingForConnection {
				// add stablished connection to the pool
				c.ConnectionPool <- connectionToServer
			} else {
				// close connection
				connectionToServer.Close()
			}
		}
	}()

	// create local listener
	listenAddress, err := net.ResolveUDPAddr("udp4", GlobalConfig.UDPListen)
	if err != nil {
		panic(err)
	}
	localListener, err := net.ListenUDP("udp4", listenAddress)
	if err != nil {
		panic(err)
	}
	defer localListener.Close()
	fmt.Println("listening on " + GlobalConfig.UDPListen)

	// handle packets from users
	b := make([]byte, 1500)
	for {
		// read packet from user
		n, userAddress, e := localListener.ReadFromUDPAddrPort(b)
		if e != nil {
			if conn, ok := c.UserAddressToConnectionTable[addrPortToString(userAddress)]; ok {
				conn.Close()
				delete(c.UserAddressToConnectionTable, addrPortToString(userAddress))
			}
			continue
		}

		// check if user has connection to server
		if conn, ok := c.UserAddressToConnectionTable[addrPortToString(userAddress)]; ok {
			_, e = conn.Write(b[:n])
			if e != nil {
				conn.Close()
				delete(c.UserAddressToConnectionTable, addrPortToString(userAddress))
			}
		} else {
			c.WaitingForConnection = true

			// wait for new connection from server
			connectionToServer := <-c.ConnectionPool

			c.WaitingForConnection = false

			// add new connection to table
			c.UserAddressToConnectionTable[addrPortToString(userAddress)] = connectionToServer

			// handle new packets from server on new go routine
			go func(userAddr netip.AddrPort, conn net.Conn, firstPacket []byte) {
				fmt.Printf("accepted new connection from %s for %s\n", conn.RemoteAddr(), userAddr)

				// write the first packet to server
				_, e = connectionToServer.Write(firstPacket)
				if e != nil {
					connectionToServer.Close()
					delete(c.UserAddressToConnectionTable, addrPortToString(userAddress))
				}

				// close connection when done
				defer delete(c.UserAddressToConnectionTable, addrPortToString(userAddress))
				defer conn.Close()

				// read packts from server
				b := make([]byte, 1500)
				var n int
				var e error
				for {
					n, e = conn.Read(b)
					if e != nil {
						return
					}
					_, e = localListener.WriteToUDPAddrPort(b[:n], userAddr)
					if e != nil {
						return
					}
				}
			}(userAddress, connectionToServer, b[:n])
		}
	}
}
