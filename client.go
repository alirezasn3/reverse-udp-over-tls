package main

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	random "math/rand"
	"net"
	"sync"
	"time"
)

type Client struct {
	MasterConnection             net.Conn
	ConnectionPool               chan net.Conn
	UserAddressToConnectionTable sync.Map
	CleaningUpMasterConnection   bool
}

func (c *Client) Run() {
	// initialize connection pool
	c.ConnectionPool = make(chan net.Conn, 1024)

	// send keep alive packet to server
	go func() {
		var e error
		var n int
		for {
			if c.MasterConnection != nil {
				randomBytes := make([]byte, 1024)
				_, e = rand.Read(randomBytes)
				if e != nil {
					panic(e)
				}
				e = c.MasterConnection.SetWriteDeadline(time.Now().Add(time.Second * 3))
				if e != nil {
					fmt.Printf("[%s] failed to set write deadline for master connection, cleaning up...\n", e.Error())
					c.CleanUpMasterConnection()
					continue
				}
				n, e = c.MasterConnection.Write(randomBytes)
				if e != nil {
					fmt.Printf("[%s] failed to write to master connection, cleaning up...\n", e.Error())
					c.CleanUpMasterConnection()
					continue
				}
				if n != 1024 {
					fmt.Printf("[%s] failed to write to master connection, cleaning up...\n", "0 bytes written")
					c.CleanUpMasterConnection()
					continue
				}
				e = c.MasterConnection.SetWriteDeadline(time.Time{})
				if e != nil {
					fmt.Printf("[%s] failed to set write deadline for master connection, cleaning up...\n", e.Error())
					c.CleanUpMasterConnection()
					continue
				}
			}
			time.Sleep(time.Millisecond * 1000)
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
				fmt.Printf("[%s] failed to accept new connection\n", e.Error())
				continue
			}

			if c.MasterConnection == nil {
				// use the first connection as the master connection
				c.MasterConnection = connectionToServer
				fmt.Println("stablished master connection to server")
			} else {
				// add stablished connection to the pool
				c.ConnectionPool <- connectionToServer
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
		n, userAddress, e := localListener.ReadFromUDP(b)
		if e != nil {
			if conn, ok := c.UserAddressToConnectionTable.LoadAndDelete(userAddress.String()); ok {
				conn.(net.Conn).Close()
			}
			continue
		}

		// check if user has connection to server
		if conn, ok := c.UserAddressToConnectionTable.Load(userAddress.String()); ok {
			_, e = conn.(net.Conn).Write(b[:n])
			if e != nil {
				conn.(net.Conn).Close()
				c.UserAddressToConnectionTable.Delete(userAddress.String())
			}
		} else {
			// check master connection
			if c.MasterConnection == nil {
				continue
			}

			// ask for new connection and handle the first packet
			go func(firstPacket []byte) {
				// ask server for new connection
				randomBytes := make([]byte, 1025+random.Intn(1023))
				_, err := rand.Read(randomBytes)
				if err != nil {
					panic(err)
				}
				_, e = c.MasterConnection.Write(randomBytes)
				if e != nil {
					fmt.Printf("[%s] failed to write to master connection, cleaning up...\n", e.Error())
					c.CleanUpMasterConnection()
					return
				}

				// wait for new connection from server
				connectionToServer := <-c.ConnectionPool

				// add new connection to table
				c.UserAddressToConnectionTable.Store(userAddress.String(), connectionToServer)

				// write the first packet to server
				_, e = connectionToServer.Write(firstPacket)
				if e != nil {
					connectionToServer.Close()
					c.UserAddressToConnectionTable.Delete(userAddress.String())
					return
				}

				// handle new packets from server
				b := make([]byte, 1500)
				var n int
				var e error
				for {
					n, e = connectionToServer.Read(b)
					if e != nil {
						break
					}
					_, e = localListener.WriteToUDP(b[:n], userAddress)
					if e != nil {
						break
					}
				}

				// close connection when done
				connectionToServer.Close()
				c.UserAddressToConnectionTable.Delete(userAddress.String())
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

	fmt.Println("master connection closed")
}
