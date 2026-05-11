package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jung-kurt/gofpdf"
)

// GenerateLicenseKey returns a cryptographically random license key in the format:
//   ENG-XXXX-XXXX-XXXX-XXXX
func GenerateLicenseKey() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	h := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("ENG-%s-%s-%s-%s", h[0:4], h[4:8], h[8:12], h[12:16])
}

// LicensePDFParams holds everything needed to render the certificate.
type LicensePDFParams struct {
	LicenseID      string
	RawKey         string
	CustomerName   string
	CustomerEmail  string
	Company        string
	Country        string
	Plan           string
	MaxSeats       int
	IssuedAt       time.Time
	ExpiresAt      *time.Time // nil = perpetual
	IssuedBy       string
	ProductVersion string
	APIEndpoint    string  // printed in the install instructions section
}

// GeneratePDF renders a professional license certificate PDF and returns the
// output file path. Saved to the user's Downloads folder by default.
func GeneratePDF(p LicensePDFParams) (string, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()

	// ── Colour palette ──────────────────────────────────────────────────────
	// Dark navy background strip for header
	navy   := [3]int{10, 25, 60}
	teal   := [3]int{0, 178, 148}
	slate  := [3]int{45, 55, 72}
	light  := [3]int{248, 249, 250}
	muted  := [3]int{120, 130, 150}

	w := 170.0 // usable width

	// ── Header banner ────────────────────────────────────────────────────────
	pdf.SetFillColor(navy[0], navy[1], navy[2])
	pdf.Rect(0, 0, 210, 52, "F")

	// Teal accent bar
	pdf.SetFillColor(teal[0], teal[1], teal[2])
	pdf.Rect(0, 49, 210, 3, "F")

	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetXY(20, 12)
	pdf.Cell(w, 10, "LOGISTICS ENGINE")

	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(180, 200, 230)
	pdf.SetXY(20, 23)
	pdf.Cell(w, 8, "Software License Certificate")

	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(130, 150, 180)
	pdf.SetXY(20, 34)
	pdf.Cell(w, 6, fmt.Sprintf("Product Version: %s    License ID: %s", p.ProductVersion, p.LicenseID))

	// ── License tier badge ───────────────────────────────────────────────────
	badgeColor := teal
	switch strings.ToLower(p.Plan) {
	case "professional":
		badgeColor = [3]int{147, 51, 234}
	case "enterprise":
		badgeColor = [3]int{245, 158, 11}
	}
	pdf.SetFillColor(badgeColor[0], badgeColor[1], badgeColor[2])
	pdf.RoundedRect(145, 14, 45, 14, 3, "1234", "F")
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(145, 17)
	pdf.CellFormat(45, 8, strings.ToUpper(p.Plan), "", 0, "C", false, 0, "")

	pdf.SetTextColor(slate[0], slate[1], slate[2])

	// ── Issued-to section ────────────────────────────────────────────────────
	pdf.SetFillColor(light[0], light[1], light[2])
	pdf.RoundedRect(20, 58, w, 38, 3, "1234", "F")

	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(muted[0], muted[1], muted[2])
	pdf.SetXY(28, 63)
	pdf.Cell(40, 5, "LICENSED TO")

	pdf.SetFont("Helvetica", "B", 15)
	pdf.SetTextColor(slate[0], slate[1], slate[2])
	pdf.SetXY(28, 70)
	pdf.Cell(w-16, 7, p.CustomerName)

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(muted[0], muted[1], muted[2])
	pdf.SetXY(28, 79)
	pdf.Cell(80, 5, p.Company)
	pdf.SetXY(28, 85)
	pdf.Cell(80, 5, fmt.Sprintf("%s   ·   %s", p.CustomerEmail, p.Country))

	// ── Details grid ────────────────────────────────────────────────────────
	pdf.SetY(105)
	details := [][2]string{
		{"Plan",         strings.Title(p.Plan)},
		{"Seats",        fmt.Sprintf("%d seat%s", p.MaxSeats, pluralS(p.MaxSeats))},
		{"Issued",       p.IssuedAt.Format("02 January 2006")},
		{"Expires",      expiryStr(p.ExpiresAt)},
		{"Issued By",    p.IssuedBy},
	}

	col := 0
	for _, d := range details {
		x := 20.0 + float64(col)*87
		y := pdf.GetY()

		pdf.SetFillColor(light[0], light[1], light[2])
		pdf.RoundedRect(x, y, 82, 20, 2, "1234", "F")

		pdf.SetFont("Helvetica", "B", 8)
		pdf.SetTextColor(muted[0], muted[1], muted[2])
		pdf.SetXY(x+4, y+4)
		pdf.Cell(74, 5, strings.ToUpper(d[0]))

		pdf.SetFont("Helvetica", "B", 11)
		pdf.SetTextColor(slate[0], slate[1], slate[2])
		pdf.SetXY(x+4, y+11)
		pdf.Cell(74, 5, d[1])

		col++
		if col == 2 {
			col = 0
			pdf.SetY(y + 26)
		}
	}

	// ── License key box ──────────────────────────────────────────────────────
	pdf.SetY(pdf.GetY() + 10)
	keyY := pdf.GetY()

	pdf.SetFillColor(navy[0], navy[1], navy[2])
	pdf.RoundedRect(20, keyY, w, 30, 3, "1234", "F")

	pdf.SetFillColor(teal[0], teal[1], teal[2])
	pdf.RoundedRect(20, keyY, 32, 30, 3, "1234", "F")
	pdf.Rect(44, keyY, 8, 30, "F") // flush right edge

	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(20, keyY+5)
	pdf.CellFormat(32, 5, "KEY", "", 0, "C", false, 0, "")

	pdf.SetFont("Courier", "B", 15)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(56, keyY+9)
	pdf.Cell(110, 10, p.RawKey)

	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(130, 150, 180)
	pdf.SetXY(56, keyY+20)
	pdf.Cell(110, 5, "Keep this key private. Required to activate and verify the installation.")

	// ── Installation instructions ─────────────────────────────────────────────
	instY := keyY + 40
	pdf.SetFillColor(light[0], light[1], light[2])
	pdf.RoundedRect(20, instY, w, 52, 3, "1234", "F")

	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetTextColor(muted[0], muted[1], muted[2])
	pdf.SetXY(28, instY+5)
	pdf.Cell(w-16, 5, "INSTALLATION")

	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(slate[0], slate[1], slate[2])

	steps := []string{
		"1.  Run the installer on your Linux server (Ubuntu 20.04+ or Debian 11+):",
		"",
		"      curl -fsSL https://raw.githubusercontent.com/" +
			"pamidu1540/logistics-engine-core/main/install.sh | sudo bash",
		"",
		"2.  When prompted, enter your License Key from the box above.",
		"3.  Follow the prompts to enter your Telegram bot token and admin ID.",
		"4.  After install, open Telegram and send /admin to configure your depot",
		"    warehouse coordinates and pricing — no additional configuration files needed.",
	}
	for i, s := range steps {
		pdf.SetXY(28, instY+13+float64(i)*5)
		pdf.Cell(w-16, 5, s)
	}

	// ── Support footer ────────────────────────────────────────────────────────
	footY := instY + 60
	pdf.SetDrawColor(220, 225, 235)
	pdf.SetLineWidth(0.3)
	pdf.Line(20, footY, 190, footY)

	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(muted[0], muted[1], muted[2])
	pdf.SetXY(20, footY+4)
	pdf.Cell(w/2, 5, fmt.Sprintf("Generated: %s", time.Now().UTC().Format("02 Jan 2006 15:04 UTC")))
	pdf.SetXY(20+w/2, footY+4)
	pdf.CellFormat(w/2, 5, "support@yourcompany.com  ·  https://yourcompany.com", "", 0, "R", false, 0, "")

	pdf.SetXY(20, footY+9)
	pdf.MultiCell(w, 4,
		"This license is non-transferable and valid only for the named licensee. "+
			"Unauthorized redistribution or use is prohibited.",
		"", "L", false)

	// ── Save file ─────────────────────────────────────────────────────────────
	homeDir, _ := os.UserHomeDir()
	downloadsDir := filepath.Join(homeDir, "Downloads")
	_ = os.MkdirAll(downloadsDir, 0o755)

	safeName := strings.ReplaceAll(p.CustomerName, " ", "_")
	filename  := fmt.Sprintf("license_%s_%s.pdf", safeName, p.IssuedAt.Format("20060102"))
	outPath   := filepath.Join(downloadsDir, filename)

	if err := pdf.OutputFileAndClose(outPath); err != nil {
		return "", fmt.Errorf("PDF write error: %w", err)
	}
	return outPath, nil
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func expiryStr(t *time.Time) string {
	if t == nil {
		return "Perpetual"
	}
	return t.Format("02 January 2006")
}

// newLicenseID returns a short human-readable license ref
func newLicenseID() string {
	return strings.ToUpper(uuid.NewString()[:8])
}
