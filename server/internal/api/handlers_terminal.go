package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		// Origin validation is handled by CSRF and authentication middleware
		return true
	},
}

type terminalResizeMessage struct {
	Type string `json:"type"`
	Cols uint32 `json:"cols"`
	Rows uint32 `json:"rows"`
}

type windowSizeMsg struct {
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

// handleServerTerminal attaches an interactive SSH PTY session over a WebSocket
// to the selected server for operator administration and troubleshooting.
func (s *Server) handleServerTerminal(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	if !websocket.IsWebSocketUpgrade(r) {
		return BadRequest("Expected WebSocket handshake for interactive terminal.")
	}

	ws, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.Log.Warn("terminal websocket upgrade failed", "server", server.ID, "error", err)
		return nil
	}
	defer ws.Close()

	actor := MustIdentity(r.Context())
	s.Log.Info("opening interactive server terminal",
		"server", server.Name,
		"server_id", server.ID,
		"user", actor.User.Email,
	)

	conn, err := s.Servers.Connect(r.Context(), server)
	if err != nil {
		errText := fmt.Sprintf("\r\n\x1b[31;1mdockdeploy:\x1b[0m Failed to connect to %s: %v\r\n", server.Name, err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(errText))
		return nil
	}
	defer conn.Release()

	sshSession, err := conn.Client().NewSession()
	if err != nil {
		errText := fmt.Sprintf("\r\n\x1b[31;1mdockdeploy:\x1b[0m Failed to open SSH session on %s: %v\r\n", server.Name, err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(errText))
		return nil
	}
	defer sshSession.Close()

	cols := 80
	rows := 24
	if c := r.URL.Query().Get("cols"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 && n <= 500 {
			cols = n
		}
	}
	if rw := r.URL.Query().Get("rows"); rw != "" {
		if n, err := strconv.Atoi(rw); err == nil && n > 0 && n <= 500 {
			rows = n
		}
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := sshSession.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		errText := fmt.Sprintf("\r\n\x1b[31;1mdockdeploy:\x1b[0m Failed to allocate PTY on %s: %v\r\n", server.Name, err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(errText))
		return nil
	}

	stdin, err := sshSession.StdinPipe()
	if err != nil {
		return nil
	}
	defer stdin.Close()

	stdout, err := sshSession.StdoutPipe()
	if err != nil {
		return nil
	}

	stderr, err := sshSession.StderrPipe()
	if err != nil {
		return nil
	}

	if err := sshSession.Shell(); err != nil {
		errText := fmt.Sprintf("\r\n\x1b[31;1mdockdeploy:\x1b[0m Failed to spawn shell on %s: %v\r\n", server.Name, err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(errText))
		return nil
	}

	var wsMu sync.Mutex
	writeToWs := func(msgType int, data []byte) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		return ws.WriteMessage(msgType, data)
	}

	closeOnce := sync.Once{}
	closeAll := func() {
		closeOnce.Do(func() {
			_ = sshSession.Close()
			_ = ws.Close()
		})
	}

	// Output pump: forward SSH output (stdout + stderr) to WebSocket
	outReader := io.MultiReader(stdout, stderr)
	go func() {
		defer closeAll()
		buf := make([]byte, 4096)
		for {
			n, err := outReader.Read(buf)
			if n > 0 {
				if wErr := writeToWs(websocket.BinaryMessage, buf[:n]); wErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Input pump: forward WebSocket keystrokes and resize events to SSH session
	go func() {
		defer closeAll()
		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				return
			}

			if msgType == websocket.TextMessage {
				var resize terminalResizeMessage
				if err := json.Unmarshal(data, &resize); err == nil && resize.Type == "resize" && resize.Cols > 0 && resize.Rows > 0 {
					payload := ssh.Marshal(windowSizeMsg{
						Cols:   resize.Cols,
						Rows:   resize.Rows,
						Width:  resize.Cols * 8,
						Height: resize.Rows * 16,
					})
					_, _ = sshSession.SendRequest("window-change", false, payload)
					continue
				}
				if _, err := stdin.Write(data); err != nil {
					return
				}
			} else if msgType == websocket.BinaryMessage {
				if _, err := stdin.Write(data); err != nil {
					return
				}
			}
		}
	}()

	// Wait for remote shell exit
	_ = sshSession.Wait()

	// Notify frontend of session termination
	_ = writeToWs(websocket.TextMessage, []byte("\r\n\x1b[33m\r\nSession closed by remote host.\x1b[0m\r\n"))
	_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shell exited"), time.Now().Add(time.Second))
	closeAll()

	return nil
}
