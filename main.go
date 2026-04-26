package main

import (
	"log"

	"github.com/felsd/kpmenu/kpmenulib"
	"golang.org/x/sys/unix"
)

func main() {
	// Suppress coredumps for the lifetime of this process. The daemon
	// holds an unlocked KeePass database in heap; a coredump would land
	// in /var/lib/systemd/coredump and contain it. Failure to set the
	// flag is non-fatal — log and continue, the rest of the hardening
	// still applies.
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		log.Printf("PR_SET_DUMPABLE failed: %v", err)
	}

	menu := kpmenulib.Initialize()

	if menu != nil {
		// Start client
		err := kpmenulib.StartClient()
		if err != nil {
			// Failed to communicate with server - start server
			err = kpmenulib.StartServer(menu)

			if err != nil {
				log.Fatal(err)
			} else {
				log.Printf("waiting for goroutines to end")
				// Wait for any goroutine (clipboard)
				menu.WaitGroup.Wait()
			}
		}
	}
}
