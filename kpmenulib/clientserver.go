package kpmenulib

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// socketPath returns the per-user UNIX domain socket path used by both
// client and server. It prefers $XDG_RUNTIME_DIR (typically /run/user/<uid>,
// mode 0700, root-owned) and falls back to /run/user/<uid> by direct lookup
// when the variable is unset (e.g. some sudo contexts). If neither resolves
// to a usable directory the caller gets an explicit error rather than a
// silent /tmp fallback, which would be world-writable.
func socketPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("runtime dir %q not usable: %v", dir, err)
	}
	return filepath.Join(dir, "kpmenu.sock"), nil
}

// Packet is the data sent by the client to the server listener
type Packet struct {
	CliArguments []string
}

// StartClient sends a packet to the server listener over the per-user UNIX
// socket. A connect failure (no daemon running, stale socket) is the normal
// signal for the caller to fall back to StartServer.
func StartClient() error {
	path, err := socketPath()
	if err != nil {
		return err
	}

	conn, err := net.Dial("unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send the packet
	enc := gob.NewEncoder(conn)
	err = enc.Encode(Packet{CliArguments: os.Args[1:]})
	return err
}

// StartServer starts to listen for client packets
func StartServer(m *Menu) (err error) {
	if m.Configuration.Flags.Daemon {
		log.Printf("Executing as daemon")
	}

	if m.Configuration.General.NoCache && !m.Configuration.Flags.Daemon {
		// Directly execute kpmenu
		if fatal := Execute(m); fatal == true {
			os.Exit(1) // Set exit code to 1 and exit
		}
	} else {
		// Handle packet request
		handlePacket := func(packet Packet) bool {
			log.Printf("received a client call with args \"%v\"", packet.CliArguments)
			m.CliArguments = packet.CliArguments
			return Show(m)
		}

		// Execute kpmenu for the first time, if not a daemon
		exit := false
		if !m.Configuration.Flags.Daemon {
			exit = Execute(m)
		}

		// If exit is false (cache on) listen for client calls
		if !exit {
			err = setupListener(m, handlePacket)
		}
	}
	return
}

func setupListener(m *Menu, handlePacket func(Packet) bool) error {
	path, err := socketPath()
	if err != nil {
		return err
	}

	// Stale-socket recovery: if a socket file exists, dial it. A successful
	// dial means another daemon is alive — refuse to clobber it. A failed
	// dial means the file is a corpse from a prior crash, so unlink and
	// proceed. There is a small TOCTOU between this probe and Listen; if
	// another daemon wins the race net.Listen will return EADDRINUSE which
	// the caller surfaces.
	if _, statErr := os.Stat(path); statErr == nil {
		if conn, dialErr := net.Dial("unix", path); dialErr == nil {
			conn.Close()
			return errors.New("another kpmenu daemon is already listening on " + path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("failed to remove stale socket %q: %v", path, err)
		}
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	unixListener := listener.(*net.UnixListener)
	defer unixListener.Close()
	defer os.Remove(path)

	// Defense-in-depth: $XDG_RUNTIME_DIR is itself 0700 root-owned in
	// systemd setups, but explicitly chmod the socket inode to 0600 so
	// the permission is on-record even if the parent dir is misconfigured.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to chmod socket %q: %v", path, err)
	}

	exit := false
	for !exit {
		if !m.Configuration.Flags.Daemon {
			// If not a daemon prepare cache time
			remainingCacheTime := m.Configuration.General.CacheTimeout - int(time.Now().Sub(m.CacheStart).Seconds())
			unixListener.SetDeadline(time.Now().Add(time.Second * time.Duration(remainingCacheTime)))
		}

		// Listen to calls
		conn, err := listener.Accept()
		if err != nil {
			netErr := err.(*net.OpError)
			if netErr.Timeout() {
				log.Print("cache timed out")
				return nil
			}
			return err
		}
		defer conn.Close()

		// Go routine to handle input
		ch := make(chan Packet)
		errCh := make(chan error)
		go func(ch chan Packet, errCh chan error) {
			dec := gob.NewDecoder(conn)
			var packet Packet
			err := dec.Decode(&packet)
			if err != nil {
				if err != io.EOF {
					errCh <- err
				} else {
					return
				}
			}
			ch <- packet
		}(ch, errCh)

		// Handle received input
		timeout := time.Tick(3 * time.Second) // Timeout of 3 seconds - to avoid problems
		select {
		case packet := <-ch:
			// Received the data
			fatal := handlePacket(packet)
			exit = (fatal && !m.Configuration.Flags.Daemon)
			break
		case err := <-errCh:
			// Received an error
			return err
		case <-timeout:
			// Timed out
			log.Printf("received request is timed out")
		}
	}

	return nil
}

