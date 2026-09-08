package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rtoms/chattui/internal/server"
)

func main() {
	port := flag.Int("port", 8443, "Port for the chatTUI server to listen on")
	dbPath := flag.String("db", "chattui.db", "Path to the SQLite database file")
	retentionMinutes := flag.Int("retention-check", 15, "Interval in minutes to clean up messages > 24 hours")
	expiryMinutes := flag.Int("expiry-check", 1, "Interval in minutes to check expired temporary rooms")
	flag.Parse()

	addr := fmt.Sprintf("0.0.0.0:%d", *port)

	log.Printf("==============================================")
	log.Printf("            chatTUI Server v1.0.0            ")
	log.Printf("==============================================")
	log.Printf("Listening Address : %s", addr)
	log.Printf("Database File     : %s", *dbPath)
	log.Printf("24h Message Prune : Every %d min", *retentionMinutes)
	log.Printf("Room Expiry Check : Every %d min", *expiryMinutes)

	srv, err := server.NewServer(server.Config{
		Addr:              addr,
		DBPath:            *dbPath,
		RetentionInterval: time.Duration(*retentionMinutes) * time.Minute,
		ExpiryInterval:    time.Duration(*expiryMinutes) * time.Minute,
	})
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %s, initiating graceful shutdown...", sig)
		srv.Stop()
		os.Exit(0)
	}()

	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
