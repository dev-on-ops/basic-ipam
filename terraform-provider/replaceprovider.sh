rm -rf .terraform
rm .terraform.lock.hcl
go build main.go
cp main ~/.terraform.d/plugins/terraform-example.com/ipam-test/ipam-test/1.0.0/darwin_arm64/terraform-provider-ipam-test
terraform init
