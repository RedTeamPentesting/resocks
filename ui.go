package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type connection struct {
	IP    net.IP
	Start time.Time
	End   time.Time
	Error string
}

type loggedError struct {
	Error error
	Time  time.Time
}

type relayConnectedMessage net.IP

func relayDisconnectedMessage(err error) rawRelayDisconnectedMessage {
	if err != nil {
		return rawRelayDisconnectedMessage(err.Error())
	}

	return rawRelayDisconnectedMessage("")
}

type rawRelayDisconnectedMessage string

type statusMessage int

const (
	statusSOCKSInactive statusMessage = 2
	statusSOCKSActive   statusMessage = 3
	statusShutdown      statusMessage = 4
)

type model struct {
	errors              []loggedError
	previousConnections []*connection
	connection          *connection
	socksActive         bool
	quitting            bool

	connectionKey string
	listenAddress string
	socksAddress  string

	noColor bool
}

var _ tea.Model = &model{}

func startUI(
	connectionKey ConnectionKey, listenAddr string, socksAddr string, noColor bool,
) (update func(tea.Msg), wait func() error) {
	program := tea.NewProgram(&model{
		connectionKey: connectionKey.String(),
		listenAddress: listenAddr,
		socksAddress:  socksAddr,
		noColor:       noColor,
	}, tea.WithoutSignalHandler(), tea.WithInput(strings.NewReader("")))

	done := make(chan struct{})

	var err error

	go func() {
		_, err = program.Run()

		close(done)
	}()

	wait = func() error {
		<-done

		return err
	}

	return program.Send, wait
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case relayConnectedMessage:
		m.connection = &connection{IP: net.IP(msg), Start: time.Now()}
	case rawRelayDisconnectedMessage:
		if m.connection != nil {
			m.connection.End = time.Now()
			m.connection.Error = string(msg)
			m.previousConnections = append(m.previousConnections, m.connection)
			m.connection = nil
		}
	case error:
		if msg != nil {
			m.errors = append(m.errors, loggedError{Error: msg, Time: time.Now()})
		}
	case statusMessage:
		switch msg {
		case statusSOCKSActive:
			m.socksActive = true
		case statusSOCKSInactive:
			m.socksActive = false
		case statusShutdown:
			m.quitting = true

			return m, tea.Quit
		}
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
			conn.IP, formatTime(conn.Start), formatTime(conn.End), conn.End.Sub(conn.Start), errMsg)
	}

	return view + "\n" + m.style()
}

func (m *model) errorsView() string {
	if len(m.errors) == 0 {
		return ""
	}

	view := m.style(bold) + "Errors:\n" + m.style() + m.style(brightYellow)

	for _, err := range m.errors {
		view += fmt.Sprintf("  ➜ %s %s\n", formatTime(err.Time), err.Error.Error())
	}

	return view + "\n" + m.style()
}

func (m *model) listenerConfigView() string {
	view := m.style(bold) + "Listening On  : " + m.style() + m.style(brightMagenta, bold) + m.listenAddress + m.style()
	view += m.style(bold) + "\nConnection Key: " + m.style() + m.style(brightMagenta, bold) + m.connectionKey + m.style()

	return view + "\n"
}

func (m *model) currentStateView() string {
	relayStatus := m.style(brightYellow) + "⧗ Waiting for the relay to connect..." + m.style()
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

const (
	escape        = "\x1b"
	reset         = 0
	bold          = 1
	dim           = 2
	green         = 32
	brightRed     = 91
	bightGreen    = 92
	brightYellow  = 93
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
