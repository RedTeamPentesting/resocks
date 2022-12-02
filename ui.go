package main

import (
	"fmt"
	"strings"
	"time"

	"resocks/pbtls"
	"resocks/proxyrelay"

	tea "github.com/charmbracelet/bubbletea"
)

type connection struct {
	IP    string
	Start time.Time
	End   time.Time
	Error string
}

type loggedError struct {
	Error error
	Time  time.Time
}

type shutdownMessage struct{}

type model struct {
	errors              []*loggedError
	previousConnections []*connection
	connection          *connection
	socksActive         bool
	insecure            bool
	quitting            bool
	dotCount            int

	connectionKey string
	listenAddress string
	socksAddress  string

	noColor bool
}

var _ tea.Model = &model{}

func startUI(
	connectionKey pbtls.ConnectionKey, listenAddr string,
	socksAddr string, insecure bool, noColor bool,
) (update func(tea.Msg), wait func() error, done chan struct{}) {
	program := tea.NewProgram(&model{
		connectionKey: connectionKey.String(),
		listenAddress: listenAddr,
		socksAddress:  socksAddr,
		insecure:      insecure,
		noColor:       noColor,
	})

	done = make(chan struct{})

	var err error

	go func() {
		_, err = program.Run()

		close(done)
	}()

	wait = func() error {
		<-done

		return err
	}

	return program.Send, wait, done
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *model) Init() tea.Cmd {
	return tick()
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case proxyrelay.Event:
		switch msg.Type {
		case proxyrelay.TypeRelayConnected:
			m.connection = &connection{IP: msg.Data, Start: time.Now()}
		case proxyrelay.TypeRelayDisconnected:
			if m.connection != nil {
				m.connection.End = time.Now()
				m.connection.Error = msg.Data
				m.previousConnections = append(m.previousConnections, m.connection)
				m.connection = nil
			}

			m.errors = nil
		case proxyrelay.TypeSOCKS5Active:
			m.socksActive = true
		case proxyrelay.TypeSOCKS5Inactive:
			m.socksActive = false
		case proxyrelay.TypeError:
			if msg.Data != "" {
				msg.Data = "unknown error"
			}

			m.errors = append(m.errors, &loggedError{Error: fmt.Errorf(msg.Data), Time: time.Now()})
		}
	case shutdownMessage:
		m.quitting = true

		return m, tea.Quit
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true

			return m, tea.Quit
		}

	case tickMsg:
		m.dotCount = (m.dotCount + 1) % 4

		return m, tick()
	}

	return m, nil
}

func (m *model) View() string {
	return m.style() + m.previousConnectionsView() + m.errorsView() +
		m.listenerConfigView() + m.currentStateView() + m.style()
}

func (m *model) previousConnectionsView() string {
	if len(m.previousConnections) == 0 {
		return ""
	}

	view := m.style(dim) + "Previous Connections:\n"

	for _, conn := range m.previousConnections {
		errMsg := ""
		if conn.Error != "" {
			errMsg = ": " + conn.Error
		}

		view += fmt.Sprintf("  ➜ %s from %s until %s (%s)%s\n",
			conn.IP, formatTime(conn.Start), formatTime(conn.End), formatDuration(conn.End.Sub(conn.Start)), errMsg)
	}

	return view + "\n" + m.style()
}

func (m *model) errorsView() string {
	if len(m.errors) == 0 {
		return ""
	}

	view := m.style(bold) + "Errors:\n" + m.style() + m.style(yellow)

	for _, err := range m.errors {
		view += fmt.Sprintf("  ➜ %s: %s\n", formatTime(err.Time), err.Error.Error())
	}

	return view + m.style() + "\n"
}

func (m *model) listenerConfigView() string {
	view := m.style(bold) + "Listening On  : " + m.style() + m.style(brightMagenta, bold) + m.listenAddress + m.style()
	view += m.style(bold) + "\nConnection Key: " + m.style() + m.style(brightMagenta, bold) + m.connectionKey + m.style()

	if m.insecure {
		view += m.style(yellow) + " (Warning: Client certificate validation disabled)" + m.style()
	}

	return view + "\n"
}

func (m *model) currentStateView() string {
	relayStatus := m.style(yellow) + "⧗ Waiting for the relay to connect" +
		strings.Repeat(".", m.dotCount) + m.style()
	if m.quitting {
		relayStatus = m.style(brightRed) + "✗ Shutdown" + m.style()
	} else if m.connection != nil {
		relayStatus = fmt.Sprintf(m.style(green)+"✔ %s connected since %s"+m.style(),
			m.connection.IP, formatTime(m.connection.Start))
	}

	socksStatus := m.style(brightRed) + "✗ Inactive" + m.style()
	if m.quitting {
		socksStatus = m.style(brightRed) + "✗ Shutdown" + m.style()
	} else if m.socksActive {
		socksStatus = fmt.Sprintf(m.style(bightGreen) + fmt.Sprintf("● Active, listening on %s", m.socksAddress) + m.style())
	}

	return fmt.Sprintf(m.style(bold)+"Current Status:\n"+m.style()+"  Relay : %s\n  SOCKS5: %s\n",
		relayStatus, socksStatus)
}

func formatTime(t time.Time) string {
	if t.Day() == time.Now().Day() {
		return t.Format("3:04:05 pm")
	}

	return t.Format("02.01.06 3:04:05 pm")
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(1 * time.Millisecond).String()
	case d < time.Minute:
		return d.Round(10 * time.Millisecond).String()
	default:
		return d.Round(1 * time.Second).String()
	}
}

const (
	escape        = "\x1b"
	reset         = 0
	bold          = 1
	dim           = 2
	green         = 32
	yellow        = 33
	brightRed     = 91
	bightGreen    = 92
	brightMagenta = 95
)

func (m *model) style(attrs ...int) string {
	if m.noColor {
		return ""
	}

	if len(attrs) == 0 {
		attrs = []int{reset}
	}

	s := ""

	for _, a := range attrs {
		s += fmt.Sprintf("%s[%dm", escape, a)
	}

	return s
}
