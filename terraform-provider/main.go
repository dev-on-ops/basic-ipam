package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	_ "log"
	"net/http"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: func() *schema.Provider {
			return provider()
		},
	})
}

func provider() *schema.Provider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"server_url": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "The URL of the IP reservation API server",
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"ipam-test_ip_reservation": resourceIP(),
		},
		ConfigureContextFunc: configureProvider,
	}
}

func configureProvider(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
	serverURL := d.Get("server_url").(string)

	return &providerConfig{
		ServerURL: serverURL,
	}, nil
}

type providerConfig struct {
	ServerURL string
}

func resourceIP() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceIPCreate,
		ReadContext:   resourceIPRead,
		DeleteContext: resourceIPDelete,
		Schema: map[string]*schema.Schema{
			"cidr": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The CIDR notation for the network",
			},
			"tenant_name": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The name of the tenant",
			},
			"purpose": {
				Type:        schema.TypeString,
				Required:    true,
				ForceNew:    true,
				Description: "The purpose of the IP (host, gateway, dns, vip)",
			},
			"ip_address": {
				Type:        schema.TypeString,
				Computed:    true,
				Description: "The reserved IP address",
			},
		},
	}
}

func resourceIPCreate(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	serverURL := m.(*providerConfig).ServerURL
	cidr := d.Get("cidr").(string)
	tenantName := d.Get("tenant_name").(string)
	purpose := d.Get("purpose").(string)

	// Validate the state of IP addresses in the subnet
	//if err := validateIPState(serverURL, cidr); err != nil {
	//	return diag.FromErr(fmt.Errorf("failed to validate IP state: %s", err))
	//}

	// Proceed with reserving the IP address
	ipAddress, err := reserveIP(serverURL, cidr, tenantName, purpose)
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to reserve IP: %s", err))
	}

	d.SetId(ipAddress)
	d.Set("ip_address", ipAddress)

	return nil
}

func resourceIPRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	serverURL := m.(*providerConfig).ServerURL
	cidr := d.Get("cidr").(string)
	//tenantName := d.Get("tenant_name").(string)
	ipAddress := d.Get("ip_address").(string)

	// Make the HTTP GET request to the get-ips-in-subnet endpoint
	resp, err := http.Get(fmt.Sprintf("%s/get-ips-in-subnet?subnet=%s", serverURL, cidr))
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to fetch IPs in subnet: %s", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return diag.FromErr(fmt.Errorf("failed to fetch IPs in subnet, status code: %d", resp.StatusCode))
	}

	var response struct {
		IPs []string `json:"ips"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return diag.FromErr(fmt.Errorf("failed to decode response body: %s", err))
	}

	// Check if the IP exists in the subnet
	ipExists := false
	for _, ip := range response.IPs {
		if ip == ipAddress {
			ipExists = true
			break
		}
	}

	if !ipExists {
		return diag.Errorf("IP address %s does not exist in subnet %s", ipAddress, cidr)
	}

	return nil
}

func resourceIPDelete(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	serverURL := m.(*providerConfig).ServerURL
	cidr := d.Get("cidr").(string)
	tenantName := d.Get("tenant_name").(string)
	ipAddress := d.Id()

	err := releaseIP(serverURL, cidr, tenantName, ipAddress)
	if err != nil {
		return diag.FromErr(fmt.Errorf("failed to release IP: %s", err))
	}

	return nil
}

//func validateIPState(serverURL, subnet string) error {
//	resp, err := http.Get(fmt.Sprintf("%s/get-ips-in-subnet?subnet=%s", serverURL, subnet))
//	if err != nil {
//		return err
//	}
//	defer resp.Body.Close()
//
//	if resp.StatusCode != http.StatusOK {
//		return fmt.Errorf("failed to validate IP state, status code: %d", resp.StatusCode)
//	}
//
//	var response struct {
//		IPs []string `json:"ips"`
//	}
//
//	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
//		return fmt.Errorf("failed to decode response body: %s", err)
//	}
//
//	// Check if the subnet is already at capacity (you can customize this logic)
//	if len(response.IPs) >= 255 {
//		return fmt.Errorf("subnet %s is at capacity", subnet)
//	}
//
//	return nil
//}

func reserveIP(serverURL, cidr, tenantName, purpose string) (string, error) {
	// Prepare the request payload
	requestPayload := map[string]string{
		"cidr":       cidr,
		"tenant_name": tenantName,
		"purpose":    purpose,
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return "", err
	}

	// Make the HTTP POST request to reserve-ip endpoint
	resp, err := http.Post(serverURL+"/reserve-ip", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to reserve IP, status code: %d", resp.StatusCode)
	}

	// Read the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Parse the response JSON
	var response struct {
		IPAddress string `json:"ip_address"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}

	return response.IPAddress, nil
}

func releaseIP(serverURL, cidr, tenantName, ipAddress string) error {
	// Prepare the request payload
	requestPayload := map[string]string{
		"cidr":        cidr,
		"tenant_name": tenantName,
		"ip_address":  ipAddress,
	}
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return err
	}

	// Make the HTTP POST request to release-ip endpoint
	resp, err := http.Post(serverURL+"/release-ip", "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to release IP, status code: %d", resp.StatusCode)
	}

	return nil
}
