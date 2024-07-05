#!/bin/bash

echo "net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.ip_forward=1" >> /etc/sysctl.conf
sysctl -p

openssl req -new -nodes -x509 -out cert -keyout key -days 3650 -subj "/C=DE/ST=NRW/L=Earth/O=Example Company/OU=IT/CN=www.example.com/emailAddress=john@example.com"

curl -OL https://golang.org/dl/go1.22.5.linux-amd64.tar.gz
tar -C /usr/local -xvf go1.22.5.linux-amd64.tar.gz
rm go1.22.5.linux-amd64.tar.gz
echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.profile
source ~/.profile

go build

cp reverse-udp-over-tls.service /etc/systemd/system/reverse-udp-over-tls.service
chmod 664 /etc/systemd/system/reverse-udp-over-tls.service
systemctl daemon-reload
systemctl enable reverse-udp-over-tls

if [ -e config.json ]
then
    systemctl start reverse-udp-over-tls
    systemctl status reverse-udp-over-tls
else
    echo "create config.json and run systemctl start reverse-udp-over-tls"
fi