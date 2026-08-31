# Reverse UDP Over TLS

## Overview

This project provides a solution for tunneling UDP traffic over a TLS connection. It includes both server and client components, allowing secure communication between endpoints.

## Features

- **Secure Communication**: Uses TLS to encrypt UDP traffic.
- **Cross-Platform**: Can be built and run on multiple platforms.
- **Server-Initiated Connections**: The server initiates connections to clients, helping to bypass some Deep Packet Inspection (DPI) tools.

## Configuration

```json
{
  "role": "server",
  "tcpConnect": "client_address:port",
  "udpConnect": "local_udp_service_address:port",
  "tcpListen": "server_address:port",
  "udpListen": "local_udp_listen_address:port",
  "secret": "tls secret",
  "clientPostDown": "command",
  "serverPostDown": "command"
}
```