# Reverse UDP Over TLS

## Overview

This project provides a solution for tunneling UDP traffic over a TLS connection. It includes both server and client components, allowing secure communication between endpoints. The project also includes a web interface for monitoring the status and statistics of the connections.

## Features

- **Secure Communication**: Uses TLS to encrypt UDP traffic.
- **Real-time Monitoring**: Web interface to monitor connection statistics.
- **Cross-Platform**: Can be built and run on multiple platforms.
- **Server-Initiated Connections**: The server initiates connections to clients, helping to bypass some Deep Packet Inspection (DPI) tools.

## Directory Structure

- `/templates`: Contains the HTML template for the web interface.
- `/main.go`: Main entry point for the application.
- `/server.go`: Contains the server logic.
- `/client.go`: Contains the client logic.
- `/install.sh`: Script for setting up the environment and installing the service.
- `/.gitignore`: Specifies files to be ignored by git.

## Prerequisites

- Go 1.20 or later
- OpenSSL

## Installation

1. **Clone the repository**:

   ```sh
   git clone https://github.com/alirezasn3/reverse-udp-over-tls.git
   cd reverse-udp-over-tls
   ```

2. **Run the installation script**:

   ```sh
   sudo ./install.sh
   ```

3. **Create a `config.json` file** in the root directory with the following structure:

   ```json
   {
     "role": "server",
     "tcpConnect": ["client_address:port"],
     "udpConnect": "local_udp_service_address:port",
     "tcpListen": "server_address:port",
     "udpListen": "local_udp_listen_address:port",
     "monitorAddress": "monitor_address:port"
   }
   ```

4. **Start the service**:
   ```sh
   sudo systemctl start reverse-udp-over-tls
   sudo systemctl status reverse-udp-over-tls
   ```

## Usage

### Running the Server

1. **Configure the server** in `config.json` with the role set to `"server"`.
2. **Start the server**:
   ```sh
   sudo systemctl start reverse-udp-over-tls
   ```

### Running the Client

1. **Configure the client** in `config.json` with the role set to `"client"`.
2. **Start the client**:
   ```sh
   sudo systemctl start reverse-udp-over-tls
   ```

### Monitoring

- Access the web interface at the `monitorAddress` specified in `config.json`.
- The interface displays real-time statistics for download and upload speeds, connection status, and more.

## Configuration

### `config.json` Fields

- **role**: `"server"` or `"client"`.
- **tcpConnect**: List of TCP addresses the server should connect to.
- **udpConnect**: Local UDP service address the server should forward packets to.
- **tcpListen**: TCP address the client should listen on.
- **udpListen**: Local UDP address the client should listen on.
- **monitorAddress**: Address for the web interface.

## Server-Initiated Connections

One of the key features of this project is that the server initiates connections to the clients. This approach helps to bypass some Deep Packet Inspection (DPI) tools that might otherwise block or throttle client-initiated connections. By having the server initiate the connection, the traffic appears to be more legitimate and is less likely to be flagged by DPI mechanisms.

### How It Works

- The server continuously attempts to create a TLS connection to the client.
- Once a connection is established, the server handles the forwarding of UDP packets between the client and the local UDP service.
- This method ensures that the server is always in control of the connection, making it harder for DPI tools to detect and block the traffic.

## Development

### Building the Project

1. **Build the project**:
   ```sh
   go build
   ```

### Running Locally

1. **Run the application**:
   ```sh
   ./reverse-udp-over-tls
   ```

## License

This project is licensed under the MIT License.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Contact

For any questions or issues, please open an issue on the [GitHub repository](https://github.com/alirezasn3/reverse-udp-over-tls).

---

This README provides a comprehensive overview of the project, including installation, usage, and configuration details. For more information, refer to the source code and comments within the files.
