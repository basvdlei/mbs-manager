package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/basvdlei/mbs-manager/pkg/bedrock"
)

var (
	listen = flag.String("listen", "localhost:8080", "Listen address")
	path   = flag.String("path", "/usr/local/bedrock-server/bedrock_server",
		"Bedrock Server binary path")
)

func main() {
	flag.Parse()
	s, errChan := bedrock.RunServer(*path, flag.Args()...)
	// Hook up the local terminal, discarding the output since we already
	// have server command logging on the stdout.
	go s.Attach(os.Stdin, io.Discard)

	http.HandleFunc("/raw", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// XXX: dangerous code, there is NO validation at all.
		command, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.SendRawCommand(string(command))
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		s.SendRawCommand("stop")
	})

	http.HandleFunc("/hold", func(w http.ResponseWriter, r *http.Request) {
		s.SendRawCommand("save hold")
	})

	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		s.SendRawCommand("save query")
	})

	http.HandleFunc("/resume", func(w http.ResponseWriter, r *http.Request) {
		s.SendRawCommand("save resume")
	})

	http.HandleFunc("/backup", func(w http.ResponseWriter, r *http.Request) {
		name := fmt.Sprintf("backup-%s.tar",
			time.Now().Format("20060102-150405"))
		w.Header().Add("Content-Description", "File Transfer")
		w.Header().Add("Content-Type", "application/octet-stream")
		w.Header().Add("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"`, name))
		opts := bedrock.BackupOptions{
			Backupper: bedrock.TarBackup{
				Writer: w,
			},
			CommandTimeout: 5 * time.Second,
			SavePause:      1 * time.Minute,
		}
		//err := s.Backup(context.Background(), bedrock.BackupOptions{
		//	Backupper: bedrock.BackupperFunc(bedrock.CopyBackup),
		//})
		err := s.Backup(context.Background(), opts)
		if err != nil {
			log.Printf("error during backup: %v", err)
		}
	})

	go func() {
		select {
		case err := <-errChan:
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}()

	log.Printf("[MBS] Listening on %s", *listen)
	http.ListenAndServe(*listen, nil)
}
