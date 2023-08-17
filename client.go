package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"time"
)

type Client struct {
	MasterConnection             net.Conn
	ConnectionPool               chan net.Conn
	UserAddressToConnectionTable sync.Map
	LastSentKeepAlivePacket      int64
	CleaningUpMasterConnection   bool
}

func (c *Client) Run() {
	// initialize connection pool
	c.ConnectionPool = make(chan net.Conn, 1024)

	// send keep alive packet to server
	go func() {
		var e error
		var diff int64
		for {
			diff = time.Now().UnixMilli() - c.LastSentKeepAlivePacket
			if diff > 2500 {
				if c.MasterConnection != nil {
					_, e = c.MasterConnection.Write([]byte{0})
					if e != nil {
						c.LastSentKeepAlivePacket = time.Now().UnixMilli()
					}
				}
			}
			time.Sleep(time.Millisecond * time.Duration(diff))
		}
	}()

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
				fmt.Printf("[%s]\nfailed to accept new connection\n", e.Error())
				continue
			}

			// handle new connection on new go routine
			go func(conn net.Conn) {
				// set read deadline for the new connection
				e := conn.SetReadDeadline(time.Now().Add(time.Second * 3))
				if e != nil {
					fmt.Printf("[%s]\nfailed to set read deadline for the new connection\n", e.Error())
					conn.Close()
				}

				// read secret from client
				b := make([]byte, len(GlobalConfig.Secret))
				n, e := conn.Read(b)
				if e != nil {
					fmt.Printf("[%s]\nfailed to read secret\n", e.Error())
					conn.Close()
				}

				// check if secret is valid
				if string(b[:n]) != GlobalConfig.Secret {
					fmt.Printf("invalid secret: %s\n", b[:n])
					conn.Close()
				}

				// send ok packet to server
				_, e = conn.Write([]byte(GlobalConfig.Secret))
				if e != nil {
					fmt.Printf("[%s]\nfailed to send secret back to server\n", e.Error())
					conn.Close()
				}

				if c.MasterConnection == nil {
					// use the first connection as the master connection
					c.MasterConnection = conn
					fmt.Println("stablished master connection to server")
				} else {
					// add stablished connection to the pool
					c.ConnectionPool <- conn
				}
			}(connectionToServer)
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
		n, userAddress, e := localListener.ReadFromUDP(b)
		if e != nil {
			if conn, ok := c.UserAddressToConnectionTable.LoadAndDelete(userAddress.String()); ok {
				conn.(net.Conn).Close()
			}
			continue
		}

		// check if user has connection to server
		if conn, _ := c.UserAddressToConnectionTable.Load(userAddress.String()); conn != nil {
			_, e = conn.(net.Conn).Write(b[:n])
			if e != nil {
				conn.(net.Conn).Close()
				c.UserAddressToConnectionTable.Delete(userAddress.String())
			}
		} else {
			// ask for new connection and handle the first packet
			go func(firstPacket []byte) {
				// check master connection
				if c.MasterConnection == nil {
					return
				}

				// ask server for new connection
				_, e = c.MasterConnection.Write([]byte{0})
				if e != nil {
					c.CleanUpMasterConnection()
					return
				}

				// set time for last sent packet
				c.LastSentKeepAlivePacket = time.Now().UnixMilli()

				// wait for new connection from server
				connectionToServer := <-c.ConnectionPool

				// add new connection to table
				c.UserAddressToConnectionTable.Store(userAddress.String(), connectionToServer)

				// handle new packets from server on new go routine
				go func(userAddr *net.UDPAddr, conn net.Conn) {
					// close connection when done
					defer func() {
						if conn != nil {
							conn.Close()
						}
						c.UserAddressToConnectionTable.Delete(userAddress.String())
					}()

					// read packts from server
					b := make([]byte, 1500)
					var n int
					var e error
					for {
						n, e = conn.Read(b)
						if e != nil {
							return
						}
						_, e = localListener.WriteToUDP(b[:n], userAddr)
						if e != nil {
							return
						}
					}
				}(userAddress, connectionToServer)

				// write the first packet to server
				_, e = connectionToServer.Write(firstPacket)
				if e != nil {
					connectionToServer.Close()
					c.UserAddressToConnectionTable.Delete(userAddress.String())
				}
			}(b[:n])
		}
	}
}

func (c *Client) CleanUpMasterConnection() {
	// check if master connection exists and not already closing
	if c.CleaningUpMasterConnection || c.MasterConnection == nil {
		return
	}

	c.CleaningUpMasterConnection = true
	c.MasterConnection.Close()
	c.MasterConnection = nil
	c.CleaningUpMasterConnection = false
}