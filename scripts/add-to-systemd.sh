#!/bin/bash

sudo cp ../reverse-udp-over-tls.service /etc/systemd/system/reverse-udp-over-tls.service
sudo chmod 664 /etc/systemd/system/reverse-udp-over-tls.service
sudo systemctl daemon-reload
sudo systemctl start reverse-udp-over-tls
sudo systemctl enable reverse-udp-over-tls
sudo systemctl status reverse-udp-over-tls