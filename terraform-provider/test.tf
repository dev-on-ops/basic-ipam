terraform {
  required_providers {
    ipam-test = {
      version = "~> 1.0.0"
      source  = "terraform-example.com/ipam-test/ipam-test"
    }
  }
}

provider "ipam-test" {
  server_url = "http://localhost:8080"  # Replace with the actual server URL
}

resource "ipam-test_ip_reservation" "example1" {
  count = 2
  cidr        = "10.0.0.0/24"
  tenant_name = "example_tenant_1"
  purpose     = "host"
}

resource "ipam-test_ip_reservation" "example2" {
  count = 2
  cidr        = "10.0.0.0/8"
  tenant_name = "example_tenant_2"
  purpose     = "host"
}