package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ── Request / response types ──────────────────────────────────────────────────

type CreateCustomerReq struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company"`
	Country string `json:"country"`
	Notes   string `json:"notes"`
}

type Customer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Company   string `json:"company"`
	Country   string `json:"country"`
	CreatedAt int64  `json:"created_at"`
}

type CreateLicenseReq struct {
	CustomerID     string `json:"customer_id"`
	RawKey         string `json:"raw_key"`
	Plan           string `json:"plan"`
	MaxSeats       int    `json:"max_seats"`
	Days           int    `json:"days,omitempty"` // 0 = perpetual
	IssuedBy       string `json:"issued_by"`
	ProductVersion string `json:"product_version"`
}

type License struct {
	ID             string `json:"id"`
	CustomerID     string `json:"customer_id"`
	CustomerName   string `json:"customer_name"`
	CustomerEmail  string `json:"customer_email"`
	Company        string `json:"company"`
	Plan           string `json:"plan"`
	MaxSeats       int    `json:"max_seats"`
	SeatsUsed      int    `json:"seats_used"`
	Status         string `json:"status"`
	IssuedAt       int64  `json:"issued_at"`
	ExpiresAt      *int64 `json:"expires_at"`
	IssuedBy       string `json:"issued_by"`
	ProductVersion string `json:"product_version"`
}

type PatchLicenseReq struct {
	Status   string `json:"status,omitempty"`
	Days     int    `json:"days,omitempty"`
	MaxSeats int    `json:"max_seats,omitempty"`
}

type Activation struct {
	ID               string  `json:"id"`
	Hostname         string  `json:"hostname"`
	IPAddress        string  `json:"ip_address"`
	InstallerVersion string  `json:"installer_version"`
	ActivatedAt      int64   `json:"activated_at"`
	LastSeenAt       int64   `json:"last_seen_at"`
	RevokedAt        *int64  `json:"revoked_at"`
}

type Event struct {
	ID        int64  `json:"id"`
	LicenseID string `json:"license_id"`
	Hostname  string `json:"hostname"`
	IPAddress string `json:"ip_address"`
	Event     string `json:"event"`
	Detail    string `json:"detail"`
	Ts        int64  `json:"ts"`
}

type Stats struct {
	Licenses struct {
		Total     int `json:"total"`
		Active    int `json:"active"`
		Suspended int `json:"suspended"`
		Revoked   int `json:"revoked"`
		Expired   int `json:"expired"`
	} `json:"licenses"`
	Customers   int `json:"customers"`
	Activations int `json:"activations"`
}

// ── API client ────────────────────────────────────────────────────────────────

type APIClient struct {
	base     string
	adminKey string
}

func NewAPIClient() *APIClient {
	return &APIClient{base: cfg.APIBase, adminKey: cfg.AdminKey}
}

func (c *APIClient) do(method, path string, body any) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Key", c.adminKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func apiErr(data []byte, status int) error {
	var e struct{ Error string `json:"error"` }
	_ = json.Unmarshal(data, &e)
	if e.Error != "" {
		return fmt.Errorf("API %d: %s", status, e.Error)
	}
	return fmt.Errorf("API error %d", status)
}

// ── Customers ─────────────────────────────────────────────────────────────────

func (c *APIClient) CreateCustomer(req CreateCustomerReq) (string, error) {
	data, status, err := c.do("POST", "/admin/customers", req)
	if err != nil {
		return "", err
	}
	if status != 201 {
		return "", apiErr(data, status)
	}
	var r struct{ ID string `json:"id"` }
	_ = json.Unmarshal(data, &r)
	return r.ID, nil
}

func (c *APIClient) ListCustomers() ([]Customer, error) {
	data, status, err := c.do("GET", "/admin/customers", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, apiErr(data, status)
	}
	var r struct{ Customers []Customer `json:"customers"` }
	_ = json.Unmarshal(data, &r)
	return r.Customers, nil
}

// ── Licenses ──────────────────────────────────────────────────────────────────

func (c *APIClient) CreateLicense(req CreateLicenseReq) (string, error) {
	data, status, err := c.do("POST", "/admin/licenses", req)
	if err != nil {
		return "", err
	}
	if status != 201 {
		return "", apiErr(data, status)
	}
	var r struct{ ID string `json:"id"` }
	_ = json.Unmarshal(data, &r)
	return r.ID, nil
}

func (c *APIClient) ListLicenses(statusFilter string) ([]License, error) {
	path := "/admin/licenses"
	if statusFilter != "" {
		path += "?status=" + statusFilter
	}
	data, status, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, apiErr(data, status)
	}
	var r struct{ Licenses []License `json:"licenses"` }
	_ = json.Unmarshal(data, &r)
	return r.Licenses, nil
}

func (c *APIClient) GetLicense(id string) (*License, []Activation, []Event, error) {
	data, status, err := c.do("GET", "/admin/licenses/"+id, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	if status != 200 {
		return nil, nil, nil, apiErr(data, status)
	}
	var r struct {
		License     License      `json:"license"`
		Activations []Activation `json:"activations"`
		Events      []Event      `json:"events"`
	}
	_ = json.Unmarshal(data, &r)
	return &r.License, r.Activations, r.Events, nil
}

func (c *APIClient) PatchLicense(id string, req PatchLicenseReq) error {
	data, status, err := c.do("PATCH", "/admin/licenses/"+id, req)
	if err != nil {
		return err
	}
	if status != 200 {
		return apiErr(data, status)
	}
	return nil
}

func (c *APIClient) GetStats() (*Stats, error) {
	data, status, err := c.do("GET", "/admin/stats", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, apiErr(data, status)
	}
	var s Stats
	_ = json.Unmarshal(data, &s)
	return &s, nil
}
