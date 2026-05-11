package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Colour tokens ─────────────────────────────────────────────────────────────

var (
	colorNavy   = lipgloss.Color("#0A193C")
	colorTeal   = lipgloss.Color("#00B294")
	colorSlate  = lipgloss.Color("#2D3748")
	colorMuted  = lipgloss.Color("#718096")
	colorLight  = lipgloss.Color("#F8F9FA")
	colorWhite  = lipgloss.Color("#FFFFFF")
	colorGold   = lipgloss.Color("#F59E0B")
	colorPurple = lipgloss.Color("#9333EA")
	colorRed    = lipgloss.Color("#EF4444")
	colorGreen  = lipgloss.Color("#10B981")
)

// ── Common styles ─────────────────────────────────────────────────────────────

var (
	titleStyle = lipgloss.NewStyle().
			Background(colorNavy).
			Foreground(colorWhite).
			Bold(true).
			Padding(1, 3)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorTeal).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorTeal).
			Padding(0, 2)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(18)

	valueStyle = lipgloss.NewStyle().
			Foreground(colorSlate).
			Bold(true)

	keyStyle = lipgloss.NewStyle().
			Background(colorNavy).
			Foreground(colorTeal).
			Bold(true).
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorTeal)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorMuted).
			PaddingTop(1).
			MarginTop(1)
)

func planBadge(plan string) string {
	switch strings.ToLower(plan) {
	case "enterprise":
		return lipgloss.NewStyle().Background(colorGold).Foreground(colorNavy).
			Bold(true).Padding(0, 1).Render(" ENTERPRISE ")
	case "professional":
		return lipgloss.NewStyle().Background(colorPurple).Foreground(colorWhite).
			Bold(true).Padding(0, 1).Render(" PROFESSIONAL ")
	default:
		return lipgloss.NewStyle().Background(colorTeal).Foreground(colorNavy).
			Bold(true).Padding(0, 1).Render(" STANDARD ")
	}
}

func statusBadge(status string) string {
	switch status {
	case "active":
		return successStyle.Render("● active")
	case "suspended":
		return lipgloss.NewStyle().Foreground(colorGold).Bold(true).Render("● suspended")
	case "revoked", "expired":
		return errorStyle.Render("● " + status)
	default:
		return mutedStyle.Render("● " + status)
	}
}

// ── Screen enum ───────────────────────────────────────────────────────────────

type screen int

const (
	screenDashboard screen = iota
	screenIssueStep1       // customer details
	screenIssueStep2       // license details
	screenIssueStep3       // confirm + issue
	screenIssueResult      // show key + PDF path
	screenListLicenses
	screenLicenseDetail
	screenSettings
)

// ── Messages ──────────────────────────────────────────────────────────────────

type errMsg        struct{ err error }
type loadedMsg     struct{ licenses []License; customers []Customer }
type issuedMsg     struct{ licenseID, rawKey, pdfPath string; err error }
type licenseDetail struct{ license *License; activations []Activation }
type patchedMsg    struct{ err error }

// ── Issue wizard form fields ──────────────────────────────────────────────────

type issueForm struct {
	// Step 1 — customer
	custName    textinput.Model
	custEmail   textinput.Model
	custCompany textinput.Model
	custCountry textinput.Model
	custNotes   textinput.Model
	existingID  string // set if reusing an existing customer
	focusIdx    int

	// Step 2 — license
	plan      int    // 0=standard 1=professional 2=enterprise
	maxSeats  textinput.Model
	days      textinput.Model // 0 = perpetual
	focusIdx2 int
}

func newIssueForm() issueForm {
	mk := func(placeholder string) textinput.Model {
		t := textinput.New()
		t.Placeholder = placeholder
		t.CharLimit    = 120
		t.PromptStyle  = accentStyle
		t.TextStyle    = valueStyle
		return t
	}
	f := issueForm{
		custName:    mk("e.g. John Smith"),
		custEmail:   mk("e.g. john@acme.com"),
		custCompany: mk("e.g. Acme Logistics"),
		custCountry: mk("e.g. LK"),
		custNotes:   mk("optional notes"),
		maxSeats:    mk("1"),
		days:        mk("365  (0 = perpetual)"),
	}
	f.maxSeats.SetValue("1")
	f.days.SetValue("365")
	f.custName.Focus()
	return f
}

// ── Root model ────────────────────────────────────────────────────────────────

type model struct {
	screen   screen
	width    int
	height   int
	spinner  spinner.Model
	loading  bool
	err      string
	info     string

	// Data
	licenses   []License
	customers  []Customer
	detailLic  *License
	detailActs []Activation

	// Tables
	licTable table.Model

	// Issue wizard
	form issueForm

	// Result
	resultKey  string
	resultPDF  string
	resultID   string
}

func newModel() model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style   = accentStyle

	return model{
		screen:  screenDashboard,
		spinner: sp,
		form:    newIssueForm(),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadData())
}

func (m model) loadData() tea.Cmd {
	return func() tea.Msg {
		client := NewAPIClient()
		licenses, err := client.ListLicenses("")
		if err != nil {
			return errMsg{err}
		}
		customers, err := client.ListCustomers()
		if err != nil {
			return errMsg{err}
		}
		return loadedMsg{licenses: licenses, customers: customers}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		m.err = ""
		m.info = ""
		switch msg.String() {
		case "ctrl+c", "q":
			if m.screen == screenDashboard {
				return m, tea.Quit
			}
			m.screen = screenDashboard
			return m, nil
		case "esc":
			m.screen = screenDashboard
			return m, nil
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case errMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case loadedMsg:
		m.loading  = false
		m.licenses = msg.licenses
		m.customers = msg.customers
		m.licTable  = buildLicTable(msg.licenses)
		return m, nil

	case licenseDetail:
		m.loading    = false
		m.detailLic  = msg.license
		m.detailActs = msg.activations
		m.screen     = screenLicenseDetail
		return m, nil

	case issuedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.screen = screenDashboard
			return m, nil
		}
		m.resultKey = msg.rawKey
		m.resultPDF = msg.pdfPath
		m.resultID  = msg.licenseID
		m.screen    = screenIssueResult
		return m, m.loadData()

	case patchedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.info = "License updated."
		}
		m.screen = screenDashboard
		return m, m.loadData()
	}

	// ── Screen-specific updates ───────────────────────────────────────────────
	switch m.screen {
	case screenDashboard:
		m, cmds = m.updateDashboard(msg, cmds)
	case screenIssueStep1:
		m, cmds = m.updateStep1(msg, cmds)
	case screenIssueStep2:
		m, cmds = m.updateStep2(msg, cmds)
	case screenIssueStep3:
		m, cmds = m.updateStep3(msg, cmds)
	case screenListLicenses:
		m, cmds = m.updateList(msg, cmds)
	case screenLicenseDetail:
		m, cmds = m.updateDetail(msg, cmds)
	}

	return m, tea.Batch(cmds...)
}

func (m model) updateDashboard(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "n", "N":
			m.form   = newIssueForm()
			m.screen = screenIssueStep1
		case "l", "L":
			m.loading = true
			m.screen  = screenListLicenses
			cmds = append(cmds, m.loadData())
		case "s", "S":
			m.screen = screenSettings
		}
	}
	return m, cmds
}

func (m model) updateStep1(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	fields := []*textinput.Model{
		&m.form.custName, &m.form.custEmail,
		&m.form.custCompany, &m.form.custCountry, &m.form.custNotes,
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "tab", "down", "enter":
			fields[m.form.focusIdx].Blur()
			m.form.focusIdx = (m.form.focusIdx + 1) % len(fields)
			if m.form.focusIdx == 0 && k.String() == "enter" {
				// All filled — advance
				if m.form.custName.Value() == "" || m.form.custEmail.Value() == "" {
					m.err = "Name and email are required."
					return m, cmds
				}
				m.form.maxSeats.Focus()
				m.screen = screenIssueStep2
				return m, cmds
			}
			fields[m.form.focusIdx].Focus()
		case "shift+tab", "up":
			fields[m.form.focusIdx].Blur()
			m.form.focusIdx = (m.form.focusIdx - 1 + len(fields)) % len(fields)
			fields[m.form.focusIdx].Focus()
		}
	}
	for i, f := range fields {
		if i == m.form.focusIdx {
			var cmd tea.Cmd
			*fields[i], cmd = f.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return m, cmds
}

func (m model) updateStep2(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	fields := []*textinput.Model{&m.form.maxSeats, &m.form.days}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "1": m.form.plan = 0
		case "2": m.form.plan = 1
		case "3": m.form.plan = 2
		case "tab", "down":
			fields[m.form.focusIdx2].Blur()
			m.form.focusIdx2 = (m.form.focusIdx2 + 1) % len(fields)
			fields[m.form.focusIdx2].Focus()
		case "shift+tab", "up":
			fields[m.form.focusIdx2].Blur()
			m.form.focusIdx2 = (m.form.focusIdx2 - 1 + len(fields)) % len(fields)
			fields[m.form.focusIdx2].Focus()
		case "enter":
			m.screen = screenIssueStep3
			return m, cmds
		case "esc":
			m.screen = screenIssueStep1
			return m, cmds
		}
	}
	for i, f := range fields {
		if i == m.form.focusIdx2 {
			var cmd tea.Cmd
			*fields[i], cmd = f.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return m, cmds
}

func (m model) updateStep3(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "y", "Y", "enter":
			m.loading = true
			cmds = append(cmds, m.issueCmd())
		case "n", "N", "esc":
			m.screen = screenDashboard
		}
	}
	return m, cmds
}

func (m model) updateList(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
    sel := m.licTable.SelectedRow()
    if len(sel) > 0 {
        id := sel[0] // This is the 'id' the compiler says is unused
        m.loading = true
        cmds = append(cmds, func() tea.Msg {
            client := NewAPIClient()
            // We use 'id' here, and use '_' to ignore 'evts' for now 
            // because your licenseDetail struct doesn't have an events field yet.
            lic, acts, _, err := client.GetLicense(id) 
            if err != nil {
                return errMsg{err}
            }
            return licenseDetail{license: lic, activations: acts}
        })
    }
		}
	}
	var cmd tea.Cmd
	m.licTable, cmd = m.licTable.Update(msg)
	cmds = append(cmds, cmd)
	return m, cmds
}

func (m model) updateDetail(msg tea.Msg, cmds []tea.Cmd) (model, []tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && m.detailLic != nil {
		id := m.detailLic.ID
		client := NewAPIClient()
		switch k.String() {
		case "s":
			m.loading = true
			cmds = append(cmds, func() tea.Msg {
				return patchedMsg{client.PatchLicense(id, PatchLicenseReq{Status: "suspended"})}
			})
		case "a":
			m.loading = true
			cmds = append(cmds, func() tea.Msg {
				return patchedMsg{client.PatchLicense(id, PatchLicenseReq{Status: "active"})}
			})
		case "r":
			m.loading = true
			cmds = append(cmds, func() tea.Msg {
				return patchedMsg{client.PatchLicense(id, PatchLicenseReq{Status: "revoked"})}
			})
		case "e":
			m.loading = true
			cmds = append(cmds, func() tea.Msg {
				return patchedMsg{client.PatchLicense(id, PatchLicenseReq{Days: 365})}
			})
		}
	}
	return m, cmds
}

func (m model) issueCmd() tea.Cmd {
	f := m.form
	plans := []string{"standard", "professional", "enterprise"}
	plan  := plans[f.plan]

	maxSeats := 1
	fmt.Sscan(f.maxSeats.Value(), &maxSeats)

	days := 365
	fmt.Sscan(f.days.Value(), &days)

	return func() tea.Msg {
		client   := NewAPIClient()
		rawKey   := GenerateLicenseKey()
		issuedAt := time.Now().UTC()

		// 1. Create or reuse customer
		customerID := f.existingID
		custName   := f.custName.Value()
		custEmail  := f.custEmail.Value()
		custCompany:= f.custCompany.Value()
		custCountry:= f.custCountry.Value()

		if customerID == "" {
			var err error
			customerID, err = client.CreateCustomer(CreateCustomerReq{
				Name: custName, Email: custEmail,
				Company: custCompany, Country: custCountry,
				Notes: f.custNotes.Value(),
			})
			if err != nil {
				return issuedMsg{err: fmt.Errorf("create customer: %w", err)}
			}
		}

		// 2. Store license in D1
		licID, err := client.CreateLicense(CreateLicenseReq{
			CustomerID:     customerID,
			RawKey:         rawKey,
			Plan:           plan,
			MaxSeats:       maxSeats,
			Days:           days,
			IssuedBy:       cfg.IssuedBy,
			ProductVersion: cfg.ProductVer,
		})
		if err != nil {
			return issuedMsg{err: fmt.Errorf("create license: %w", err)}
		}

		// 3. Generate PDF certificate
		var expiresAt *time.Time
		if days > 0 {
			t := issuedAt.Add(time.Duration(days) * 24 * time.Hour)
			expiresAt = &t
		}

		pdfPath, err := GeneratePDF(LicensePDFParams{
			LicenseID:      licID,
			RawKey:         rawKey,
			CustomerName:   custName,
			CustomerEmail:  custEmail,
			Company:        custCompany,
			Country:        custCountry,
			Plan:           plan,
			MaxSeats:       maxSeats,
			IssuedAt:       issuedAt,
			ExpiresAt:      expiresAt,
			IssuedBy:       cfg.IssuedBy,
			ProductVersion: cfg.ProductVer,
			APIEndpoint:    cfg.APIBase,
		})
		if err != nil {
			return issuedMsg{err: fmt.Errorf("PDF generation: %w", err)}
		}

		return issuedMsg{licenseID: licID, rawKey: rawKey, pdfPath: pdfPath}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.loading {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			accentStyle.Render(m.spinner.View()+" Processing…"))
	}

	var view string
	switch m.screen {
	case screenDashboard:
		view = m.viewDashboard()
	case screenIssueStep1:
		view = m.viewStep1()
	case screenIssueStep2:
		view = m.viewStep2()
	case screenIssueStep3:
		view = m.viewStep3()
	case screenIssueResult:
		view = m.viewResult()
	case screenListLicenses:
		view = m.viewList()
	case screenLicenseDetail:
		view = m.viewDetail()
	case screenSettings:
		view = m.viewSettings()
	default:
		view = m.viewDashboard()
	}

	if m.err != "" {
		view += "\n" + errorStyle.Render("✘  "+m.err)
	}
	if m.info != "" {
		view += "\n" + successStyle.Render("✔  "+m.info)
	}
	return view
}

func header(title string) string {
	return lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("🚚  LOGISTICS ENGINE  —  LICENSE MANAGER"),
		accentStyle.Render("    "+title),
		"",
	)
}

func (m model) viewDashboard() string {
	active, total := 0, len(m.licenses)
	for _, l := range m.licenses {
		if l.Status == "active" {
			active++
		}
	}
	stats := boxStyle.Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			fmt.Sprintf("%s\n%s",
				mutedStyle.Render("TOTAL LICENSES"),
				valueStyle.Render(fmt.Sprintf("%d", total))),
			"          ",
			fmt.Sprintf("%s\n%s",
				mutedStyle.Render("ACTIVE"),
				successStyle.Render(fmt.Sprintf("%d", active))),
			"          ",
			fmt.Sprintf("%s\n%s",
				mutedStyle.Render("CUSTOMERS"),
				valueStyle.Render(fmt.Sprintf("%d", len(m.customers)))),
		),
	)

	menu := lipgloss.JoinVertical(lipgloss.Left,
		accentStyle.Render("n")+" — Issue new license",
		accentStyle.Render("l")+" — List all licenses",
		accentStyle.Render("s")+" — Settings",
		accentStyle.Render("q")+" — Quit",
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		header("Dashboard"),
		stats, "",
		menu,
		footerStyle.Render("Logistics Engine License Manager  ·  "+cfg.APIBase),
	)
}

func (m model) viewStep1() string {
	fields := []struct {
		label string
		model textinput.Model
	}{
		{"Customer Name *", m.form.custName},
		{"Email *",         m.form.custEmail},
		{"Company",         m.form.custCompany},
		{"Country",         m.form.custCountry},
		{"Notes",           m.form.custNotes},
	}
	rows := make([]string, len(fields))
	for i, f := range fields {
		rows[i] = lipgloss.JoinHorizontal(lipgloss.Top,
			labelStyle.Render(f.label),
			f.model.View(),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		header("Issue License — Step 1: Customer Details"),
		lipgloss.JoinVertical(lipgloss.Left, rows...),
		"",
		footerStyle.Render("Tab/↓ next field  ·  Shift+Tab/↑ previous  ·  Enter on last field to continue  ·  Esc back"),
	)
}

func (m model) viewStep2() string {
	plans := []string{"Standard", "Professional", "Enterprise"}
	planRows := make([]string, len(plans))
	for i, p := range plans {
		prefix := "  "
		if i == m.form.plan {
			prefix = accentStyle.Render("▶ ")
		}
		planRows[i] = prefix + planBadge(strings.ToLower(p))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		header("Issue License — Step 2: License Details"),
		mutedStyle.Render("Plan  (press 1/2/3 to select):"),
		lipgloss.JoinVertical(lipgloss.Left, planRows...),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top,
			labelStyle.Render("Max Seats"),
			m.form.maxSeats.View(),
		),
		lipgloss.JoinHorizontal(lipgloss.Top,
			labelStyle.Render("Valid Days"),
			m.form.days.View(),
		),
		"",
		footerStyle.Render("1/2/3 select plan  ·  Tab next field  ·  Enter confirm  ·  Esc back"),
	)
}

func (m model) viewStep3() string {
	plans := []string{"standard", "professional", "enterprise"}
	days  := 365
	seats := 1
	fmt.Sscan(m.form.days.Value(), &days)
	fmt.Sscan(m.form.maxSeats.Value(), &seats)

	expiry := "perpetual"
	if days > 0 {
		expiry = time.Now().AddDate(0, 0, days).Format("02 Jan 2006")
	}

	summary := boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		mutedStyle.Render("Customer    ")+valueStyle.Render(m.form.custName.Value()),
		mutedStyle.Render("Email       ")+valueStyle.Render(m.form.custEmail.Value()),
		mutedStyle.Render("Company     ")+valueStyle.Render(m.form.custCompany.Value()),
		mutedStyle.Render("Plan        ")+planBadge(plans[m.form.plan]),
		mutedStyle.Render("Seats       ")+valueStyle.Render(fmt.Sprint(seats)),
		mutedStyle.Render("Expires     ")+valueStyle.Render(expiry),
	))

	return lipgloss.JoinVertical(lipgloss.Left,
		header("Issue License — Step 3: Confirm"),
		summary, "",
		accentStyle.Render("This will:"),
		"  • Create customer record in Cloudflare D1",
		"  • Store hashed license key in D1",
		"  • Generate and save PDF certificate to ~/Downloads",
		"",
		lipgloss.NewStyle().Bold(true).Render("Confirm? [y/n]"),
	)
}

func (m model) viewResult() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		header("License Issued Successfully"),
		successStyle.Render("✔  License created and stored in Cloudflare D1"),
		"",
		mutedStyle.Render("License ID:"),
		valueStyle.Render("  "+m.resultID),
		"",
		mutedStyle.Render("License Key (send this to the customer):"),
		keyStyle.Render("  "+m.resultKey),
		"",
		mutedStyle.Render("PDF Certificate:"),
		accentStyle.Render("  "+m.resultPDF),
		"",
		footerStyle.Render("Esc / q — back to dashboard"),
	)
}

func (m model) viewList() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		header("All Licenses"),
		m.licTable.View(),
		footerStyle.Render("↑↓ navigate  ·  Enter view details  ·  Esc back"),
	)
}

func (m model) viewDetail() string {
	if m.detailLic == nil {
		return header("License Detail") + "\n" + mutedStyle.Render("No license selected.")
	}
	l := m.detailLic

	expStr := "Perpetual"
	if l.ExpiresAt != nil {
		expStr = time.Unix(*l.ExpiresAt, 0).Format("02 Jan 2006")
	}

	info := boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		mutedStyle.Render("Customer    ")+valueStyle.Render(l.CustomerName)+" "+mutedStyle.Render("<"+l.CustomerEmail+">"),
		mutedStyle.Render("Company     ")+valueStyle.Render(l.Company),
		mutedStyle.Render("Plan        ")+planBadge(l.Plan),
		mutedStyle.Render("Status      ")+statusBadge(l.Status),
		mutedStyle.Render("Seats       ")+valueStyle.Render(fmt.Sprintf("%d / %d used", l.SeatsUsed, l.MaxSeats)),
		mutedStyle.Render("Expires     ")+valueStyle.Render(expStr),
		mutedStyle.Render("Issued by   ")+valueStyle.Render(l.IssuedBy),
		mutedStyle.Render("Version     ")+valueStyle.Render(l.ProductVersion),
	))

	actLines := []string{mutedStyle.Render("Activations:")}
	for _, a := range m.detailActs {
		revoked := ""
		if a.RevokedAt != nil {
			revoked = errorStyle.Render(" [REVOKED]")
		}
		actLines = append(actLines, fmt.Sprintf("  • %s  %s  last seen %s%s",
			valueStyle.Render(a.Hostname),
			mutedStyle.Render(a.IPAddress),
			mutedStyle.Render(time.Unix(a.LastSeenAt, 0).Format("02 Jan 15:04")),
			revoked,
		))
	}
	if len(m.detailActs) == 0 {
		actLines = append(actLines, mutedStyle.Render("  No activations yet."))
	}

	actions := footerStyle.Render(
		"[a] activate  [s] suspend  [r] revoke  [e] extend 365 days  ·  Esc back",
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		header("License Detail — "+l.ID),
		info, "",
		lipgloss.JoinVertical(lipgloss.Left, actLines...),
		actions,
	)
}

func (m model) viewSettings() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		header("Settings"),
		boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			mutedStyle.Render("Config file: ~/.config/logistics-license-manager/config.toml"),
			"",
			labelStyle.Render("API Base")+valueStyle.Render(cfg.APIBase),
			labelStyle.Render("Admin Key")+valueStyle.Render(strings.Repeat("•", len(cfg.AdminKey))),
			labelStyle.Render("Issued By")+valueStyle.Render(cfg.IssuedBy),
			labelStyle.Render("Product Ver")+valueStyle.Render(cfg.ProductVer),
		)),
		"",
		mutedStyle.Render("Edit ~/.config/logistics-license-manager/config.toml to change settings."),
		footerStyle.Render("Esc back"),
	)
}

// ── Table builder ─────────────────────────────────────────────────────────────

func buildLicTable(licenses []License) table.Model {
	cols := []table.Column{
		{Title: "ID",       Width: 10},
		{Title: "Customer", Width: 22},
		{Title: "Plan",     Width: 14},
		{Title: "Status",   Width: 12},
		{Title: "Seats",    Width: 8},
		{Title: "Expires",  Width: 14},
	}

	rows := make([]table.Row, len(licenses))
	for i, l := range licenses {
		exp := "perpetual"
		if l.ExpiresAt != nil {
			exp = time.Unix(*l.ExpiresAt, 0).Format("02 Jan 2006")
		}
		rows[i] = table.Row{
			l.ID[:8] + "…",
			l.CustomerName,
			l.Plan,
			l.Status,
			fmt.Sprintf("%d/%d", l.SeatsUsed, l.MaxSeats),
			exp,
		}
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(20),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorTeal).
		BorderBottom(true).
		Foreground(colorTeal).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(colorNavy).
		Background(colorTeal).
		Bold(true)
	t.SetStyles(s)
	return t
}
