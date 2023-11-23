package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

var (
	db    *sql.DB
	mutex sync.Mutex
)

func main() {
	var err error
	db, err = sql.Open("sqlite3", "ip_database.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createIPTable()

	http.HandleFunc("/reserve-ip", ReserveIPHandler)
	http.HandleFunc("/release-ip", ReleaseIPHandler)
	http.HandleFunc("/get-ips-in-subnet", GetIPsInSubnetHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server running on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func createIPTable() {
	createTable := `
		CREATE TABLE IF NOT EXISTS ips (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cidr TEXT,
			tenant_name TEXT,
			ip_address TEXT,
			purpose TEXT,
			UNIQUE (cidr, tenant_name, ip_address)
		)`
	_, err := db.Exec(createTable)
	if err != nil {
		log.Fatal(err)
	}
}

func ReserveIPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var requestPayload struct {
		CIDR       string `json:"cidr"`
		TenantName string `json:"tenant_name"`
		Purpose    string `json:"purpose"`
	}

	err := json.NewDecoder(r.Body).Decode(&requestPayload)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	mutex.Lock()
	defer mutex.Unlock()

	ip, reserved, err := reserveIP(requestPayload.CIDR, requestPayload.TenantName, requestPayload.Purpose)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reserving IP: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	response := struct {
		IPAddress string `json:"ip_address"`
		Reserved  bool   `json:"reserved"`
	}{
		IPAddress: ip,
		Reserved:  reserved,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

func reserveIP(cidr, tenantName, purpose string) (string, bool, error) {
	ip, available := findAvailableIP(cidr, tenantName)
	if !available {
		return "", false, fmt.Errorf("No available IP in the given CIDR")
	}

	// Check if the IP is already reserved
	if isIPReserved(ip, cidr, tenantName) {
		return "", false, fmt.Errorf("IP already reserved for the given CIDR and tenant")
	}

	// Reserve the IP in the database
	_, err := db.Exec("INSERT INTO ips (cidr, tenant_name, ip_address, purpose) VALUES (?, ?, ?, ?)", cidr, tenantName, ip, purpose)
	if err != nil {
		return "", false, err
	}

	return ip, true, nil
}

func ReleaseIPHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var requestPayload struct {
		CIDR       string `json:"cidr"`
		TenantName string `json:"tenant_name"`
		IPAddress  string `json:"ip_address"`
	}

	err := json.NewDecoder(r.Body).Decode(&requestPayload)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	success, err := releaseReservedIP(requestPayload.CIDR, requestPayload.TenantName, requestPayload.IPAddress)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error releasing IP: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	response := struct {
		Success bool `json:"success"`
	}{
		Success: success,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

func releaseReservedIP(cidr, tenantName, ipAddress string) (bool, error) {
	// Release the reservation in the database
	result, err := db.Exec("DELETE FROM ips WHERE cidr = ? AND tenant_name = ? AND ip_address = ?", cidr, tenantName, ipAddress)
	if err != nil {
		return false, err
	}

	// Check if any row was affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

func findAvailableIP(cidr, tenantName string) (string, bool) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", false
	}

	// Loop through IP addresses in the CIDR range starting from the second address
	for ip := incrementIP(ip.Mask(ipNet.Mask)); ipNet.Contains(ip); incrementIP(ip) {
		ipString := ip.String()

		// Check if IP is in the database
		if !isIPReserved(ipString, cidr, tenantName) {
			// IP is available
			return ipString, true
		}
	}

	// No available IP found
	return "", false
}

func isIPReserved(ip, cidr, tenantName string) bool {
	query := "SELECT ip_address FROM ips WHERE cidr = ? AND ip_address = ?"
	if tenantName != "" {
		query += " AND tenant_name = ?"
	}
	row := db.QueryRow(query, cidr, ip, tenantName)

	var storedIP string
	err := row.Scan(&storedIP)
	if err != nil && err != sql.ErrNoRows {
		log.Println("Error checking database:", err)
	}

	return storedIP == ip
}

func incrementIP(ip net.IP) net.IP {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
	return ip
}
func GetIPsInSubnetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	subnetQueryParam := r.URL.Query().Get("subnet")
	if subnetQueryParam == "" {
		http.Error(w, "Subnet parameter is required", http.StatusBadRequest)
		return
	}

	// Parse the subnet from the query parameter
	subnet := subnetQueryParam
	if !strings.Contains(subnet, "/") {
		subnet += "/32" // Assume a single IP address if no subnet mask is provided
	}

	ips, err := getIPsInSubnet(subnet)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting IPs in subnet: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	response := struct {
		IPs []string `json:"ips"`
	}{
		IPs: ips,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseJSON)
}

func getIPsInSubnet(subnet string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, err
	}
	if ip != nil {}
	rows, err := db.Query("SELECT ip_address FROM ips WHERE cidr = ?", ipNet.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []string
	for rows.Next() {
		var ipAddress string
		err := rows.Scan(&ipAddress)
		if err != nil {
			return nil, err
		}
		ips = append(ips, ipAddress)
	}

	return ips, nil
}