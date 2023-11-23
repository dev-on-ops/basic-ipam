#!/bin/bash


curl -X POST -H "Content-Type: application/json" -d '{"cidr": "192.168.1.0/24", "tenant_name": "example-tenant", "purpose": "host"}' http://localhost:8080/reserve-ip


curl -X POST -H "Content-Type: application/json" -d '{"cidr": "192.168.1.0/24", "tenant_name": "example-tenant", "ip_address": "192.168.1.2"}' http://localhost:8080/release-ip

