package ui

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/zivkotp/zivko-dhcp/internal/control"
	"github.com/zivkotp/zivko-dhcp/internal/ipcalc"
	"github.com/zivkotp/zivko-dhcp/internal/model"
	"github.com/zivkotp/zivko-dhcp/internal/runtime"
	"github.com/zivkotp/zivko-dhcp/internal/store"
	"github.com/zivkotp/zivko-dhcp/internal/validation"
)

type App struct {
	ctx          context.Context
	repo         store.Repository
	control      *control.Client
	cfg          model.Config
	embeddedOpts runtime.Options
	embeddedStop context.CancelFunc
	window       fyne.Window
	status       *widget.Label
	daemonStatus *widget.Label
	tabs         *container.AppTabs
	leaseTable   *widget.Table
	leaseBody    *fyne.Container
}

func NewApp(repo store.Repository, ctx context.Context, embeddedOpts runtime.Options, embeddedStop context.CancelFunc) *App {
	return &App{
		ctx:          ctx,
		repo:         repo,
		control:      &control.Client{},
		embeddedOpts: embeddedOpts,
		embeddedStop: embeddedStop,
	}
}

func (a *App) Run() error {
	cfg, err := a.repo.Load(context.Background())
	if err != nil {
		return err
	}
	a.cfg = cfg
	a.updateControlSocket()

	fyneApp := app.New()
	a.window = fyneApp.NewWindow("DHCP GUI")
	a.window.Resize(fyne.NewSize(1240, 780))
	a.status = widget.NewLabel("Bereit")
	a.daemonStatus = widget.NewLabel(a.formatDaemonStatusLine(false, a.effectiveRuntimeServerIP(), runtime.NormalizeListenAddr(a.cfg.Runtime.ListenAddr)))
	a.tabs = container.NewAppTabs()
	a.leaseTable = a.buildLeaseTable()
	a.leaseBody = container.NewMax(adaptiveTableSection(len(a.cfg.Leases), a.leaseTable, "Keine aktiven Leases vorhanden.", 1180))

	settingsButton := widget.NewButton("Server Settings", func() {
		a.openSettingsWindow()
	})
	configButton := widget.NewButton("Config JSON", func() {
		a.openConfigWindow()
	})
	restartButton := widget.NewButton("Server neu starten", func() {
		a.restartService()
	})

	topBar := container.NewBorder(
		nil,
		nil,
		container.NewHBox(settingsButton, configButton),
		container.NewHBox(restartButton, a.daemonStatus),
		nil,
	)

	poolsPane := container.NewBorder(nil, nil, nil, nil, a.tabs)

	bottomPane := container.NewBorder(nil, nil, nil, nil, a.leaseBody)
	topContent := container.NewVScroll(container.NewVBox(topBar, poolsPane, a.status))
	mainSplit := container.NewVSplit(topContent, bottomPane)
	mainSplit.Offset = 0.72

	a.window.SetContent(mainSplit)
	a.rebuildTabs(len(a.cfg.Pools) - 1)
	a.loadStateFromDaemon()
	a.refreshPreview()
	a.startDaemonSync()
	a.window.ShowAndRun()
	return nil
}

func (a *App) Shutdown() {
	if a.embeddedStop != nil {
		a.embeddedStop()
		a.embeddedStop = nil
	}
}

func (a *App) rebuildTabs(selectIndex int) {
	items := make([]*container.TabItem, 0, len(a.cfg.Pools)+1)
	for idx := range a.cfg.Pools {
		poolIndex := idx
		items = append(items, container.NewTabItem(a.tabTitle(a.cfg.Pools[poolIndex]), a.buildPoolTab(poolIndex)))
	}
	items = append(items, container.NewTabItem("+", a.buildAddTab()))
	a.tabs.SetItems(items)
	if len(a.cfg.Pools) == 0 {
		a.tabs.SelectIndex(0)
		return
	}
	if selectIndex < 0 {
		selectIndex = 0
	}
	if selectIndex >= len(a.cfg.Pools) {
		selectIndex = len(a.cfg.Pools) - 1
	}
	if len(a.cfg.Pools) > 0 {
		a.tabs.SelectIndex(selectIndex)
	}
}

func (a *App) buildAddTab() fyne.CanvasObject {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Name des neuen Pools")
	subnetEntry := widget.NewEntry()
	subnetEntry.SetPlaceHolder("192.168.30.0/24")
	subnetEntry.SetText("192.168.30.0/24")

	addButton := widget.NewButton("Neuen Pool anlegen", func() {
		a.addPoolWithSubnet(nameEntry.Text, subnetEntry.Text)
	})
	addButton.Importance = widget.HighImportance

	form := container.NewVBox(
		widget.NewRichTextFromMarkdown("### Neuer Pool"),
		widget.NewLabel("Name und Subnetz eingeben. Danach wird der Pool mit passenden Default-Werten angelegt und direkt geöffnet."),
		nameEntry,
		subnetEntry,
		addButton,
	)

	return container.NewCenter(container.NewPadded(form))
}

func (a *App) buildPoolTab(poolIndex int) fyne.CanvasObject {
	pool := a.cfg.Pools[poolIndex]

	nameEntry := widget.NewEntry()
	nameEntry.SetText(pool.Name)

	subnetEntry := widget.NewEntry()
	if pool.Subnet != nil {
		subnetEntry.SetText(pool.Subnet.String())
	}

	rangeStartEntry := widget.NewEntry()
	rangeStartEntry.SetText(ipString(pool.Range.Start))

	rangeEndEntry := widget.NewEntry()
	rangeEndEntry.SetText(ipString(pool.Range.End))

	gatewayEntry := widget.NewEntry()
	gatewayEntry.SetPlaceHolder("192.168.x.1")
	gatewayEntry.SetText(ipString(pool.DefaultGateway))

	dnsServersEntry := widget.NewEntry()
	dnsServersEntry.SetPlaceHolder("192.168.x.1, 1.1.1.1")
	dnsServersEntry.SetText(ipListString(pool.DNSServers))

	domainNameEntry := widget.NewEntry()
	domainNameEntry.SetPlaceHolder("example.internal")
	domainNameEntry.SetText(pool.DomainName)

	exclusionStartEntry := widget.NewEntry()
	exclusionStartEntry.SetPlaceHolder("Start-IP")
	exclusionEndEntry := widget.NewEntry()
	exclusionEndEntry.SetPlaceHolder("End-IP")

	reservationIPEntry := widget.NewEntry()
	reservationIPEntry.SetPlaceHolder("IP-Adresse")
	reservationMACEntry := widget.NewEntry()
	reservationMACEntry.SetPlaceHolder("MAC-Adresse")
	reservationHostEntry := widget.NewEntry()
	reservationHostEntry.SetPlaceHolder("Hostname")

	exclusionList := widget.NewList(
		func() int {
			return len(a.exclusionsForPool(pool.ID))
		},
		func() fyne.CanvasObject {
			return container.NewBorder(
				nil,
				nil,
				nil,
				widget.NewButton("Entfernen", nil),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			exclusions := a.exclusionsForPool(pool.ID)
			exclusion := exclusions[id]
			row := object.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%s - %s", ipString(exclusion.Range.Start), ipString(exclusion.Range.End)))
			row.Objects[1].(*widget.Button).OnTapped = func() {
				nextCfg := a.cfg
				nextCfg.Exclusions = removeExclusion(nextCfg.Exclusions, exclusion.ID)
				if a.saveCandidateConfig(nextCfg) {
					a.rebuildTabs(poolIndex)
				}
			}
		},
	)
	exclusionContent := adaptiveListSection(
		len(a.exclusionsForPool(pool.ID)),
		exclusionList,
		"Keine Ausnahmen konfiguriert.",
		320,
	)

	reservationList := widget.NewList(
		func() int {
			return len(a.reservationsForPool(pool.ID))
		},
		func() fyne.CanvasObject {
			return container.NewBorder(
				nil,
				nil,
				nil,
				widget.NewButton("Entfernen", nil),
				widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			reservations := a.reservationsForPool(pool.ID)
			reservation := reservations[id]
			row := object.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%s | %s | %s", ipString(reservation.IPAddress), reservation.MAC, reservation.Hostname))
			row.Objects[1].(*widget.Button).OnTapped = func() {
				nextCfg := a.cfg
				nextCfg.Reservations = removeReservation(nextCfg.Reservations, reservation.ID)
				if a.saveCandidateConfig(nextCfg) {
					a.rebuildTabs(poolIndex)
				}
			}
		},
	)
	reservationContent := adaptiveListSection(
		len(a.reservationsForPool(pool.ID)),
		reservationList,
		"Keine festen Zuweisungen konfiguriert.",
		320,
	)

	savePoolButton := widget.NewButton("Pool speichern", func() {
		updatedPool, err := readPoolForm(
			pool.ID,
			nameEntry.Text,
			subnetEntry.Text,
			rangeStartEntry.Text,
			rangeEndEntry.Text,
			gatewayEntry.Text,
			dnsServersEntry.Text,
			domainNameEntry.Text,
		)
		if err != nil {
			a.setStatusError(err)
			return
		}
		nextCfg := a.cfg
		nextCfg.Pools = append([]model.Pool(nil), a.cfg.Pools...)
		nextCfg.Pools[poolIndex] = updatedPool
		if a.saveCandidateConfig(nextCfg) {
			a.rebuildTabs(poolIndex)
		}
	})

	deletePoolButton := widget.NewButton("Pool löschen", func() {
		nextCfg := a.cfg
		a.deletePoolFromConfig(&nextCfg, pool.ID)
		if a.saveCandidateConfig(nextCfg) {
			a.rebuildTabs(poolIndex - 1)
		}
	})
	deletePoolButton.Importance = widget.DangerImportance

	addExclusionButton := widget.NewButton("Ausnahme hinzufügen", func() {
		rng, err := readRange(exclusionStartEntry.Text, exclusionEndEntry.Text)
		if err != nil {
			a.setStatusError(err)
			return
		}
		nextCfg := a.cfg
		nextCfg.Exclusions = append(append([]model.Exclusion(nil), a.cfg.Exclusions...), model.Exclusion{
			ID:     nextID("ex", len(a.cfg.Exclusions)+1),
			PoolID: pool.ID,
			Range:  rng,
		})
		if a.saveCandidateConfig(nextCfg) {
			exclusionStartEntry.SetText("")
			exclusionEndEntry.SetText("")
			a.rebuildTabs(poolIndex)
		}
	})

	addReservationButton := widget.NewButton("Zuweisung hinzufügen", func() {
		reservation, err := readReservation(pool.ID, reservationIPEntry.Text, reservationMACEntry.Text, reservationHostEntry.Text, len(a.cfg.Reservations)+1)
		if err != nil {
			a.setStatusError(err)
			return
		}
		nextCfg := a.cfg
		nextCfg.Reservations = append(append([]model.Reservation(nil), a.cfg.Reservations...), reservation)
		if a.saveCandidateConfig(nextCfg) {
			reservationIPEntry.SetText("")
			reservationMACEntry.SetText("")
			reservationHostEntry.SetText("")
			a.rebuildTabs(poolIndex)
		}
	})

	poolForm := widget.NewForm(
		widget.NewFormItem("Poolname", nameEntry),
		widget.NewFormItem("Subnetz", subnetEntry),
		widget.NewFormItem("Range Start", rangeStartEntry),
		widget.NewFormItem("Range Ende", rangeEndEntry),
		widget.NewFormItem("Default Gateway", gatewayEntry),
		widget.NewFormItem("DNS Server", dnsServersEntry),
		widget.NewFormItem("Domain Name", domainNameEntry),
	)

	exclusionSection := container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("### Ausnahmen"),
			widget.NewForm(
				widget.NewFormItem("Start", exclusionStartEntry),
				widget.NewFormItem("Ende", exclusionEndEntry),
			),
			addExclusionButton,
		),
		nil,
		nil,
		nil,
		exclusionContent,
	)

	reservationSection := container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("### Feste Zuweisungen"),
			widget.NewForm(
				widget.NewFormItem("IP", reservationIPEntry),
				widget.NewFormItem("MAC", reservationMACEntry),
				widget.NewFormItem("Hostname", reservationHostEntry),
			),
			addReservationButton,
		),
		nil,
		nil,
		nil,
		reservationContent,
	)

	leftPane := container.NewPadded(container.NewVBox(
		poolForm,
		container.NewHBox(savePoolButton, deletePoolButton),
	))
	rightPane := container.NewPadded(container.NewVBox(
		exclusionSection,
		reservationSection,
	))
	content := container.NewGridWithColumns(2, leftPane, rightPane)

	return container.NewPadded(content)
}

func (a *App) buildLeaseTable() *widget.Table {
	headers := []string{"Pool", "IP", "Hostname", "MAC", "Dauer", "Ablauf", "Vendor", "Client-ID", "Last Seen"}
	table := widget.NewTable(
		func() (int, int) {
			return len(a.cfg.Leases) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				label.SetText(headers[id.Col])
				return
			}
			label.TextStyle = fyne.TextStyle{}
			lease := a.cfg.Leases[id.Row-1]
			label.SetText(a.leaseCell(lease, id.Col))
		},
	)
	table.SetColumnWidth(0, 110)
	table.SetColumnWidth(1, 115)
	table.SetColumnWidth(2, 140)
	table.SetColumnWidth(3, 135)
	table.SetColumnWidth(4, 90)
	table.SetColumnWidth(5, 140)
	table.SetColumnWidth(6, 110)
	table.SetColumnWidth(7, 120)
	table.SetColumnWidth(8, 120)
	return table
}

func adaptiveListSection(itemCount int, list *widget.List, emptyMessage string, width float32) fyne.CanvasObject {
	if itemCount == 0 {
		return container.NewPadded(widget.NewLabel(emptyMessage))
	}

	scroll := container.NewVScroll(list)
	height := float32(itemCount*38 + 12)
	if height < 76 {
		height = 76
	}
	if height > 190 {
		height = 190
	}
	scroll.SetMinSize(fyne.NewSize(width, height))
	return scroll
}

func adaptiveTableSection(itemCount int, table *widget.Table, emptyMessage string, width float32) fyne.CanvasObject {
	if itemCount == 0 {
		return container.NewPadded(widget.NewLabel(emptyMessage))
	}

	scroll := container.NewVScroll(table)
	height := float32(itemCount*34 + 46)
	if height < 90 {
		height = 90
	}
	if height > 220 {
		height = 220
	}
	scroll.SetMinSize(fyne.NewSize(width, height))
	return scroll
}

func (a *App) addPoolWithSubnet(name, subnetRaw string) {
	name = strings.TrimSpace(name)
	if name == "" {
		a.setStatusError(fmt.Errorf("pool name is required"))
		return
	}
	subnetRaw = strings.TrimSpace(subnetRaw)
	if subnetRaw == "" {
		a.setStatusError(fmt.Errorf("subnet is required"))
		return
	}

	poolDefaults, err := newPoolDefaults(name, subnetRaw)
	if err != nil {
		a.setStatusError(err)
		return
	}

	poolID := nextID("pool", len(a.cfg.Pools)+1)
	nextCfg := a.cfg
	poolDefaults.ID = poolID
	nextCfg.Pools = append(append([]model.Pool(nil), a.cfg.Pools...), poolDefaults)
	if a.saveCandidateConfig(nextCfg) {
		a.rebuildTabs(len(nextCfg.Pools) - 1)
	}
}

func (a *App) deletePoolFromConfig(cfg *model.Config, poolID string) {
	filteredPools := make([]model.Pool, 0, len(cfg.Pools))
	for _, pool := range cfg.Pools {
		if pool.ID != poolID {
			filteredPools = append(filteredPools, pool)
		}
	}
	cfg.Pools = filteredPools

	filteredExclusions := make([]model.Exclusion, 0, len(cfg.Exclusions))
	for _, exclusion := range cfg.Exclusions {
		if exclusion.PoolID != poolID {
			filteredExclusions = append(filteredExclusions, exclusion)
		}
	}
	cfg.Exclusions = filteredExclusions

	filteredReservations := make([]model.Reservation, 0, len(cfg.Reservations))
	for _, reservation := range cfg.Reservations {
		if reservation.PoolID != poolID {
			filteredReservations = append(filteredReservations, reservation)
		}
	}
	cfg.Reservations = filteredReservations

	filteredLeases := make([]model.Lease, 0, len(cfg.Leases))
	for _, lease := range cfg.Leases {
		if lease.PoolID != poolID {
			filteredLeases = append(filteredLeases, lease)
		}
	}
	cfg.Leases = filteredLeases
}

func (a *App) exclusionsForPool(poolID string) []model.Exclusion {
	var out []model.Exclusion
	for _, exclusion := range a.cfg.Exclusions {
		if exclusion.PoolID == poolID {
			out = append(out, exclusion)
		}
	}
	return out
}

func (a *App) reservationsForPool(poolID string) []model.Reservation {
	var out []model.Reservation
	for _, reservation := range a.cfg.Reservations {
		if reservation.PoolID == poolID {
			out = append(out, reservation)
		}
	}
	return out
}

func (a *App) leasesForPool(poolID string) []model.Lease {
	var out []model.Lease
	for _, lease := range a.cfg.Leases {
		if lease.PoolID == poolID {
			out = append(out, lease)
		}
	}
	return out
}

func removeExclusion(exclusions []model.Exclusion, exclusionID string) []model.Exclusion {
	filtered := make([]model.Exclusion, 0, len(exclusions))
	for _, exclusion := range exclusions {
		if exclusion.ID != exclusionID {
			filtered = append(filtered, exclusion)
		}
	}
	return filtered
}

func removeReservation(reservations []model.Reservation, reservationID string) []model.Reservation {
	filtered := make([]model.Reservation, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.ID != reservationID {
			filtered = append(filtered, reservation)
		}
	}
	return filtered
}

func (a *App) saveCandidateConfig(nextCfg model.Config) bool {
	if err := validation.ValidateConfig(nextCfg); err != nil {
		a.setStatusError(err)
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if a.control != nil {
		if err := a.control.SaveState(ctx, nextCfg); err == nil {
			_ = a.repo.Save(context.Background(), nextCfg)
			a.cfg = nextCfg
			a.updateControlSocket()
			a.refreshPreview()
			return true
		}
	}

	if err := a.repo.Save(context.Background(), nextCfg); err != nil {
		a.setStatusError(err)
		return false
	}
	a.cfg = nextCfg
	a.updateControlSocket()
	a.refreshPreview()
	return true
}

func (a *App) openSettingsWindow() {
	window := fyne.CurrentApp().NewWindow("Server Settings")
	window.Resize(fyne.NewSize(620, 420))

	interfaces, err := runtime.DetectInterfaces()
	if err != nil {
		a.setStatusError(err)
		return
	}

	preferredInterface := runtime.PreferredInterfaceName()
	preferredInterfaceIP := runtime.DetectInterfaceIPv4(preferredInterface)
	defaultSocketPath, socketErr := control.DefaultSocketPath()
	if socketErr != nil {
		defaultSocketPath = ""
	}

	portEntry := widget.NewSelectEntry([]string{runtime.DefaultListenPort})
	portEntry.SetText(runtime.ListenPort(a.cfg.Runtime.ListenAddr))
	portEntry.SetPlaceHolder(runtime.DefaultListenPort)

	autoInterfaceLabel := "Automatisch"
	if preferredInterface != "" {
		if preferredInterfaceIP != "" {
			autoInterfaceLabel = fmt.Sprintf("Automatisch (%s / %s)", preferredInterface, preferredInterfaceIP)
		} else {
			autoInterfaceLabel = fmt.Sprintf("Automatisch (%s)", preferredInterface)
		}
	}

	interfaceOptions := []string{autoInterfaceLabel}
	interfaceLabels := map[string]string{"": autoInterfaceLabel}
	interfaceOrder := []string{""}
	for _, iface := range interfaces {
		label := iface.Name
		if iface.IPv4 != "" {
			label = fmt.Sprintf("%s (%s)", iface.Name, iface.IPv4)
		}
		if !iface.Up {
			label += " [down]"
		}
		if iface.Loopback {
			label += " [loopback]"
		}
		interfaceOptions = append(interfaceOptions, label)
		interfaceLabels[iface.Name] = label
		interfaceOrder = append(interfaceOrder, iface.Name)
	}

	interfaceSelect := widget.NewSelect(interfaceOptions, nil)
	selectedInterface := strings.TrimSpace(a.cfg.Runtime.InterfaceName)
	if interfaceLabels[selectedInterface] == "" {
		selectedInterface = ""
	}
	interfaceSelect.SetSelected(interfaceLabels[selectedInterface])

	interfaceInfoLabel := widget.NewLabel("")
	interfaceInfoLabel.Wrapping = fyne.TextWrapWord

	serverIPEntry := widget.NewEntry()
	serverIPEntry.SetPlaceHolder("wird automatisch vom Interface abgeleitet")
	serverIPMode := widget.NewSelect([]string{"Automatisch", "Manuell"}, nil)

	controlSocketEntry := widget.NewEntry()
	controlSocketEntry.SetPlaceHolder("wird automatisch gewählt")
	socketMode := widget.NewSelect([]string{"Automatisch", "Manuell"}, nil)

	effectiveListenLabel := widget.NewLabel("")
	socketInfoLabel := widget.NewLabel("")
	socketInfoLabel.Wrapping = fyne.TextWrapWord

	currentDetectedIP := func() string {
		return runtime.DetectInterfaceIPv4(selectedInterface)
	}
	refreshListenLabel := func() {
		effectiveListenLabel.SetText("Effektive Listen-Adresse: " + runtime.NormalizeListenAddr(portEntry.Text))
	}
	effectiveSocketPath := func() string {
		if strings.TrimSpace(controlSocketEntry.Text) != "" {
			return strings.TrimSpace(controlSocketEntry.Text)
		}
		return defaultSocketPath
	}
	refreshSocketFields := func() {
		if socketMode.Selected == "Automatisch" {
			controlSocketEntry.SetText(defaultSocketPath)
			controlSocketEntry.Disable()
			if defaultSocketPath == "" {
				socketInfoLabel.SetText("Automatischer Socket konnte nicht bestimmt werden.")
				return
			}
			socketInfoLabel.SetText("Verwendeter Socket: " + defaultSocketPath)
			return
		}
		controlSocketEntry.Enable()
		socketInfoLabel.SetText("Verwendeter Socket: " + effectiveSocketPath())
	}
	refreshInterfaceInfo := func() {
		detectedIP := currentDetectedIP()
		displayInterface := selectedInterface
		if selectedInterface == "" {
			displayInterface = preferredInterface
			detectedIP = preferredInterfaceIP
		}

		if displayInterface == "" {
			interfaceInfoLabel.SetText("Kein passendes Interface erkannt. IP bleibt auf 0.0.0.0, bis ein Interface verfügbar ist.")
		} else if detectedIP == "" {
			interfaceInfoLabel.SetText("Ausgewähltes Interface hat derzeit keine IPv4-Adresse.")
		} else {
			interfaceInfoLabel.SetText(fmt.Sprintf("Verwendetes Interface: %s, erkannte IPv4: %s", displayInterface, detectedIP))
		}

		if serverIPMode.Selected == "Automatisch" {
			serverIPEntry.SetText(detectedIP)
		}
	}
	refreshServerIPMode := func() {
		if serverIPMode.Selected == "Automatisch" {
			serverIPEntry.Disable()
			refreshInterfaceInfo()
			return
		}
		serverIPEntry.Enable()
	}

	if strings.TrimSpace(a.cfg.Runtime.ServerIP) == "" {
		serverIPMode.SetSelected("Automatisch")
		serverIPEntry.SetText(currentDetectedIP())
	} else {
		serverIPMode.SetSelected("Manuell")
		serverIPEntry.SetText(strings.TrimSpace(a.cfg.Runtime.ServerIP))
	}
	serverIPMode.OnChanged = func(string) {
		refreshServerIPMode()
	}

	if strings.TrimSpace(a.cfg.Runtime.ControlSocket) == "" {
		socketMode.SetSelected("Automatisch")
		controlSocketEntry.SetText(defaultSocketPath)
	} else {
		socketMode.SetSelected("Manuell")
		controlSocketEntry.SetText(strings.TrimSpace(a.cfg.Runtime.ControlSocket))
	}
	socketMode.OnChanged = func(string) {
		refreshSocketFields()
	}

	interfaceSelect.OnChanged = func(option string) {
		for _, name := range interfaceOrder {
			if interfaceLabels[name] == option {
				selectedInterface = name
				break
			}
		}
		refreshInterfaceInfo()
	}

	portEntry.OnChanged = func(string) {
		refreshListenLabel()
	}

	saveButton := widget.NewButton("Settings speichern", func() {
		port := strings.TrimSpace(portEntry.Text)
		if port == "" {
			port = runtime.DefaultListenPort
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			a.setStatusError(fmt.Errorf("listen port must be between 1 and 65535"))
			return
		}
		serverIP := strings.TrimSpace(serverIPEntry.Text)
		if serverIPMode.Selected == "Automatisch" {
			serverIP = ""
		}
		controlSocket := strings.TrimSpace(controlSocketEntry.Text)
		if socketMode.Selected == "Automatisch" {
			controlSocket = ""
		}
		nextCfg := a.cfg
		nextCfg.Runtime = model.RuntimeSettings{
			ListenAddr:    runtime.NormalizeListenAddr(port),
			ServerIP:      serverIP,
			InterfaceName: strings.TrimSpace(selectedInterface),
			ControlSocket: controlSocket,
		}
		if a.saveCandidateConfig(nextCfg) {
			window.Close()
		}
	})

	info := widget.NewLabel("Diese Werte werden in der Config-Datei gespeichert. Interface, Server-IP und der lokale Control-Endpunkt können automatisch ermittelt werden. Flags oder Umgebungsvariablen haben weiterhin Vorrang.")
	info.Wrapping = fyne.TextWrapWord

	form := widget.NewForm(
		widget.NewFormItem("Listen Port", portEntry),
		widget.NewFormItem("", effectiveListenLabel),
		widget.NewFormItem("Interface", interfaceSelect),
		widget.NewFormItem("", interfaceInfoLabel),
		widget.NewFormItem("Server-IP Modus", serverIPMode),
		widget.NewFormItem("Server IP", serverIPEntry),
		widget.NewFormItem("Endpoint Modus", socketMode),
		widget.NewFormItem("Control Endpoint", controlSocketEntry),
		widget.NewFormItem("", socketInfoLabel),
	)

	refreshListenLabel()
	refreshServerIPMode()
	refreshSocketFields()
	refreshInterfaceInfo()

	window.SetContent(container.NewPadded(container.NewVBox(
		widget.NewRichTextFromMarkdown("## Server Settings"),
		info,
		form,
		saveButton,
	)))
	window.Show()
}

func (a *App) openConfigWindow() {
	window := fyne.CurrentApp().NewWindow("Config JSON")
	window.Resize(fyne.NewSize(760, 620))

	pathLabel := widget.NewLabel("Datei: " + a.configPath())
	infoLabel := widget.NewLabel("JSON kann hier angesehen, neu geladen, bearbeitet und angewendet werden.")
	infoLabel.Wrapping = fyne.TextWrapWord

	editor := widget.NewMultiLineEntry()
	editor.Wrapping = fyne.TextWrapWord

	loadCurrentIntoEditor := func() {
		data, err := store.MarshalConfig(a.cfg)
		if err != nil {
			a.setStatusError(err)
			return
		}
		editor.SetText(string(data))
	}

	reloadFromDisk := func() {
		cfg, err := a.repo.Load(context.Background())
		if err != nil {
			a.setStatusError(err)
			return
		}
		a.cfg = cfg
		a.rebuildTabs(0)
		a.refreshPreview()
		loadCurrentIntoEditor()
	}

	applyFromEditor := func() {
		cfg, err := store.UnmarshalConfig([]byte(editor.Text))
		if err != nil {
			a.setStatusError(err)
			return
		}
		if !a.saveCandidateConfig(cfg) {
			return
		}
		a.rebuildTabs(0)
		loadCurrentIntoEditor()
	}

	writeEditorToDisk := func() {
		cfg, err := store.UnmarshalConfig([]byte(editor.Text))
		if err != nil {
			a.setStatusError(err)
			return
		}
		if err := a.repo.Save(context.Background(), cfg); err != nil {
			a.setStatusError(err)
			return
		}
		a.cfg = cfg
		a.rebuildTabs(0)
		a.refreshPreview()
		loadCurrentIntoEditor()
	}

	openFileExternally := func() {
		path := a.configPath()
		if path == "" {
			a.setStatusError(fmt.Errorf("config path is unavailable"))
			return
		}
		a.status.SetText("Config-Datei: " + path)
	}

	buttons := container.NewGridWithColumns(
		5,
		widget.NewButton("Von Disk neu laden", reloadFromDisk),
		widget.NewButton("GUI-Stand laden", loadCurrentIntoEditor),
		widget.NewButton("JSON anwenden", applyFromEditor),
		widget.NewButton("Auf Disk schreiben", writeEditorToDisk),
		widget.NewButton("Pfad anzeigen", openFileExternally),
	)

	loadCurrentIntoEditor()

	window.SetContent(container.NewBorder(
		container.NewVBox(
			widget.NewRichTextFromMarkdown("## Konfigurationsdatei"),
			pathLabel,
			infoLabel,
			buttons,
		),
		nil,
		nil,
		nil,
		editor,
	))
	window.Show()
}

func (a *App) configPath() string {
	if fileRepo, ok := a.repo.(*store.FileRepository); ok {
		return fileRepo.Path()
	}
	path, err := store.DefaultConfigPath()
	if err != nil {
		return ""
	}
	return path
}

func (a *App) refreshPreview() {
	a.status.SetText("Konfiguration validiert")
	if a.leaseTable != nil {
		a.leaseTable.Refresh()
	}
	if a.leaseBody != nil {
		a.leaseBody.Objects = []fyne.CanvasObject{
			adaptiveTableSection(len(a.cfg.Leases), a.leaseTable, "Keine aktiven Leases vorhanden.", 1180),
		}
		a.leaseBody.Refresh()
	}
}

func (a *App) startDaemonSync() {
	a.loadStateFromDaemon()
	a.syncFromDaemon()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if a.window == nil {
				return
			}
			a.syncFromDaemon()
		}
	}()
}

func (a *App) loadStateFromDaemon() {
	if a.control == nil || !a.refreshReachableControlSocket() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg, err := a.control.State(ctx)
	if err != nil {
		a.updateDaemonStatus(a.formatDaemonStatusLine(false, a.effectiveRuntimeServerIP(), runtime.NormalizeListenAddr(a.cfg.Runtime.ListenAddr)))
		return
	}
	a.cfg = cfg
	a.updateControlSocket()
	a.rebuildTabs(0)
	if err := a.repo.Save(context.Background(), cfg); err != nil {
		a.status.SetText(fmt.Sprintf("Warnung: lokaler Cache konnte nicht geschrieben werden: %v", err))
	}
}

func (a *App) syncFromDaemon() {
	if a.control == nil || !a.refreshReachableControlSocket() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health, err := a.control.Health(ctx)
	if err != nil {
		a.updateDaemonStatus(a.formatDaemonStatusLine(false, a.effectiveRuntimeServerIP(), runtime.NormalizeListenAddr(a.cfg.Runtime.ListenAddr)))
		return
	}
	leases, err := a.control.Leases(ctx)
	if err != nil {
		a.updateDaemonStatus(a.formatDaemonStatusLine(false, health.ServerIP, health.DHCPAddr))
		return
	}

	a.daemonStatus.SetText(a.formatDaemonStatusLine(health.Status == "ok", health.ServerIP, health.DHCPAddr))
	a.cfg.Leases = leases
	a.refreshPreview()
}

func (a *App) updateDaemonStatus(status string) {
	if a.daemonStatus == nil {
		return
	}
	a.daemonStatus.SetText(status)
}

func (a *App) restartService() {
	if goruntime.GOOS == "windows" {
		if err := a.restartEmbeddedDaemon(); err != nil {
			a.setStatusError(err)
		}
		return
	}
	if a.systemdServiceActive() {
		a.restartSystemdService()
		return
	}
	if err := a.restartEmbeddedDaemon(); err != nil {
		a.setStatusError(err)
		return
	}
}

func (a *App) restartSystemdService() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pkexec", "systemctl", "restart", "zivko-dhcp-daemon.service")
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		a.setStatusError(fmt.Errorf("service restart failed: %s", message))
		return
	}

	a.status.SetText("Service wurde neu gestartet")
	a.loadStateFromDaemon()
	a.syncFromDaemon()
}

func (a *App) restartEmbeddedDaemon() error {
	if a.embeddedStop == nil {
		return fmt.Errorf("kein laufender systemd-service und kein eingebetteter daemon aktiv")
	}
	a.embeddedStop()

	cancelEmbedded, err := runtime.StartEmbeddedServices(a.runtimeContext(), a.embeddedOpts)
	if err != nil {
		a.embeddedStop = nil
		return fmt.Errorf("embedded daemon restart failed: %w", err)
	}
	a.embeddedStop = cancelEmbedded
	a.status.SetText("Embedded-Daemon wurde neu gestartet")
	a.loadStateFromDaemon()
	a.syncFromDaemon()
	return nil
}

func (a *App) systemdServiceActive() bool {
	if goruntime.GOOS == "windows" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "zivko-dhcp-daemon.service")
	return cmd.Run() == nil
}

func (a *App) runtimeContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) effectiveRuntimeServerIP() string {
	serverIP := strings.TrimSpace(a.cfg.Runtime.ServerIP)
	if serverIP != "" {
		return serverIP
	}
	detected := runtime.DetectInterfaceIPv4(strings.TrimSpace(a.cfg.Runtime.InterfaceName))
	if detected != "" {
		return detected
	}
	return "0.0.0.0"
}

func (a *App) formatDaemonStatusLine(active bool, serverIP, listenAddr string) string {
	state := "inaktiv"
	if active {
		state = "aktiv"
	}
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		serverIP = a.effectiveRuntimeServerIP()
	}
	port := runtime.ListenPort(listenAddr)
	return fmt.Sprintf("Status: %s - %s:%s", state, serverIP, port)
}

func (a *App) updateControlSocket() {
	if a.control == nil {
		return
	}
	socketPath := strings.TrimSpace(a.cfg.Runtime.ControlSocket)
	if socketPath == "" {
		defaultPath, err := control.DefaultSocketPath()
		if err == nil {
			socketPath = defaultPath
		}
	}
	a.control.SocketPath = socketPath
}

func (a *App) refreshReachableControlSocket() bool {
	if a.control == nil {
		return false
	}

	candidates := make([]string, 0, 3)
	seen := map[string]struct{}{}
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	addCandidate(a.control.SocketPath)
	addCandidate(a.cfg.Runtime.ControlSocket)
	addCandidate(control.SystemSocketPath)

	if fallback, err := control.DefaultSocketPath(); err == nil {
		addCandidate(fallback)
	}

	for _, candidate := range candidates {
		client := &control.Client{SocketPath: candidate}
		ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
		_, err := client.Health(ctx)
		cancel()
		if err == nil {
			a.control.SocketPath = candidate
			return true
		}
	}

	if len(candidates) > 0 {
		a.control.SocketPath = candidates[0]
	}
	return false
}

func (a *App) setStatusError(err error) {
	a.status.SetText(fmt.Sprintf("Fehler: %v", err))
	if a.window != nil {
		dialog.ShowError(err, a.window)
	}
}

func (a *App) tabTitle(pool model.Pool) string {
	if strings.TrimSpace(pool.Name) == "" {
		return "Unbenannt"
	}
	return pool.Name
}

func (a *App) leaseCell(lease model.Lease, col int) string {
	switch col {
	case 0:
		return a.poolName(lease.PoolID)
	case 1:
		return ipString(lease.IPAddress)
	case 2:
		return lease.Hostname
	case 3:
		return lease.MAC
	case 4:
		return lease.Duration.String()
	case 5:
		return lease.ExpiresAt.Format("2006-01-02 15:04")
	case 6:
		return lease.Vendor
	case 7:
		return lease.ClientID
	case 8:
		return humanLastSeen(lease.LastSeenAt)
	default:
		return ""
	}
}

func (a *App) poolName(poolID string) string {
	for _, pool := range a.cfg.Pools {
		if pool.ID == poolID {
			return a.tabTitle(pool)
		}
	}
	return poolID
}

func readPoolForm(poolID, name, subnetRaw, startRaw, endRaw, gatewayRaw, dnsServersRaw, domainNameRaw string) (model.Pool, error) {
	if strings.TrimSpace(name) == "" {
		return model.Pool{}, fmt.Errorf("pool name is required")
	}
	_, subnet, err := net.ParseCIDR(strings.TrimSpace(subnetRaw))
	if err != nil {
		return model.Pool{}, fmt.Errorf("invalid subnet: %w", err)
	}
	rng, err := readRange(startRaw, endRaw)
	if err != nil {
		return model.Pool{}, err
	}
	gateway, err := readOptionalIP(gatewayRaw)
	if err != nil {
		return model.Pool{}, fmt.Errorf("invalid default gateway: %w", err)
	}
	dnsServers, err := readIPList(dnsServersRaw)
	if err != nil {
		return model.Pool{}, fmt.Errorf("invalid dns servers: %w", err)
	}
	return model.Pool{
		ID:             poolID,
		Name:           strings.TrimSpace(name),
		Subnet:         subnet,
		Range:          rng,
		DefaultGateway: gateway,
		DNSServers:     dnsServers,
		DomainName:     strings.TrimSpace(domainNameRaw),
	}, nil
}

func readRange(startRaw, endRaw string) (model.IPv4Range, error) {
	start := net.ParseIP(strings.TrimSpace(startRaw))
	if start == nil {
		return model.IPv4Range{}, fmt.Errorf("invalid start ip")
	}
	end := net.ParseIP(strings.TrimSpace(endRaw))
	if end == nil {
		return model.IPv4Range{}, fmt.Errorf("invalid end ip")
	}
	return model.IPv4Range{
		Start: start,
		End:   end,
	}, nil
}

func readReservation(poolID, ipRaw, macRaw, hostRaw string, index int) (model.Reservation, error) {
	ip := net.ParseIP(strings.TrimSpace(ipRaw))
	if ip == nil {
		return model.Reservation{}, fmt.Errorf("invalid reservation ip")
	}
	mac := strings.TrimSpace(macRaw)
	host := strings.TrimSpace(hostRaw)
	if mac == "" && host == "" {
		return model.Reservation{}, fmt.Errorf("mac address or hostname is required")
	}
	return model.Reservation{
		ID:        nextID("res", index),
		PoolID:    poolID,
		Hostname:  host,
		MAC:       mac,
		IPAddress: ip,
	}, nil
}

func readOptionalIP(raw string) (net.IP, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	return ip, nil
}

func readIPList(raw string) ([]net.IP, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	out := make([]net.IP, 0, len(parts))
	for _, part := range parts {
		ip, err := readOptionalIP(part)
		if err != nil {
			return nil, err
		}
		if ip != nil {
			out = append(out, ip)
		}
	}
	return out, nil
}

func newPoolDefaults(name, subnetRaw string) (model.Pool, error) {
	_, subnet, err := net.ParseCIDR(strings.TrimSpace(subnetRaw))
	if err != nil {
		return model.Pool{}, fmt.Errorf("invalid subnet: %w", err)
	}

	networkIP, err := ipcalc.NormalizeIPv4(subnet.IP)
	if err != nil {
		return model.Pool{}, fmt.Errorf("invalid subnet: %w", err)
	}

	maskIP := net.IP(subnet.Mask).To4()
	if maskIP == nil {
		return model.Pool{}, fmt.Errorf("invalid subnet mask")
	}

	network := ipcalc.IPToUint32(networkIP)
	mask := ipcalc.IPToUint32(maskIP)
	broadcast := network | ^mask

	firstUsable := network
	lastUsable := broadcast
	if broadcast > network+1 {
		firstUsable = network + 1
		lastUsable = broadcast - 1
	}
	if lastUsable < firstUsable {
		lastUsable = firstUsable
	}

	gateway := firstUsable
	rangeStart := firstUsable
	if gateway < lastUsable {
		rangeStart = gateway + 1
	}

	usableCount := lastUsable - firstUsable + 1
	if usableCount > 20 && rangeStart+8 <= lastUsable {
		rangeStart += 8
	}

	rangeEnd := lastUsable
	if usableCount > 30 && rangeEnd > 10 {
		candidate := rangeEnd - 10
		if candidate >= rangeStart {
			rangeEnd = candidate
		}
	}
	if rangeEnd < rangeStart {
		rangeEnd = rangeStart
	}

	gatewayIP := ipcalc.Uint32ToIP(gateway)
	return model.Pool{
		Name:           strings.TrimSpace(name),
		Subnet:         subnet,
		DefaultGateway: gatewayIP,
		DNSServers:     []net.IP{gatewayIP},
		DomainName:     "example.internal",
		Range: model.IPv4Range{
			Start: ipcalc.Uint32ToIP(rangeStart),
			End:   ipcalc.Uint32ToIP(rangeEnd),
		},
	}, nil
}

func nextID(prefix string, index int) string {
	return fmt.Sprintf("%s-%03d", prefix, index)
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

func ipListString(ips []net.IP) string {
	if len(ips) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != nil {
			parts = append(parts, ip.String())
		}
	}
	return strings.Join(parts, ", ")
}

func humanLastSeen(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	diff := time.Since(ts).Round(time.Second)
	return diff.String() + " ago"
}
