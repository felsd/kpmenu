package kpmenulib

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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

// installSignalHandler arranges for the daemon to exit cleanly on SIGTERM
// or SIGINT, unlinking the socket file so the next start does not have to
// dial-probe a stale path. Without this, an external `pkill kpmenu` (e.g.
// from a screen-lock hook) would leave a socket corpse behind.
func installSignalHandler(listener net.Listener, path string) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-c
		log.Printf("received %s, shutting down", s)
		listener.Close()
		os.Remove(path)
		os.Exit(0)
	}()
}

// checkPeerUID verifies the connecting process runs under the same UID as
// the server. Linux's SO_PEERCRED returns the credentials of the peer at
// the moment of connect. Go's net.UnixConn does not expose the underlying
// fd directly, so we go through SyscallConn().Control to call
// getsockopt(2) inside a callback that has the fd in scope.
func checkPeerUID(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("syscall conn: %v", err)
	}

	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return fmt.Errorf("control fd: %v", err)
	}
	if credErr != nil {
		return fmt.Errorf("getsockopt SO_PEERCRED: %v", credErr)
	}

	if cred.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("peer uid=%d pid=%d does not match server uid=%d",
			cred.Uid, cred.Pid, os.Getuid())
	}
	return nil
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

	installSignalHandler(unixListener, path)

	exit := false
	for !exit {
		if !m.Configuration.Flags.Daemon {
			// Sliding-window idle timeout. Each successful Accept resets the
			// deadline to a fresh CacheTimeout from now, so as long as the
			// user keeps accessing the database the daemon stays alive.
			// CacheOneTime users (who explicitly want fixed expiry) can
			// still get that — Show() leaves CacheStart untouched and the
			// stale-database check in Show() forces a re-prompt on next
			// access.
			unixListener.SetDeadline(time.Now().Add(time.Second * time.Duration(m.Configuration.General.CacheTimeout)))
		}

		// Listen to calls
		conn, err := unixListener.AcceptUnix()
		if err != nil {
			netErr := err.(*net.OpError)
			if netErr.Timeout() {
				log.Print("cache timed out")
				return nil
			}
			return err
		}

		// Reject connections from any UID other than the server's own.
		// This is belt-and-braces with the 0600 file mode but defends
		// against future weakening of $XDG_RUNTIME_DIR permissions or
		// fd-passing that might bypass the fs check.
		if err := checkPeerUID(conn); err != nil {
			log.Printf("refusing connection: %v", err)
			conn.Close()
			continue
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

