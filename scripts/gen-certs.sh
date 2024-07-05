#!/bin/bash

openssl req -new -nodes -x509 -out ../cert -keyout ../key -days 3650 -subj "/C=DE/ST=NRW/L=Earth/O=Example Company/OU=IT/CN=www.example.com/emailAddress=john@example.com"