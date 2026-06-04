package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

var logger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	seg := os.Getenv("LINKO_LOG_FILE")
	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	if status != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", status)
		os.Exit(1)
	}
	initializeLogger(seg)

}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) error {
	f, err := os.OpenFile("linko.access.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	var accessLogger = log.New(f, "INFO: ", log.LstdFlags)
	var standardLogger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
	st, err := store.New(dataDir, standardLogger)
	if err != nil {
		accessLogger.Printf("failed to create store: %v", err)
		return fmt.Errorf("failed created store")
	}
	s := newServer(*st, httpPort, cancel, accessLogger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		standardLogger.Printf("failed to shutdown server: %v\n", err)
		return fmt.Errorf("error shutdown")
	}
	if serverErr != nil {
		accessLogger.Printf("server error: %v\n", serverErr)
		return fmt.Errorf("error server")
	}
	standardLogger.Println("Linko is shutting down")
	return nil
}

func initializeLogger(logFile string) (*log.Logger, error) {
	if logFile != "" {

		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err

		}
		multiWriter := io.MultiWriter(os.Stderr, f)
		logger := log.New(multiWriter, "", log.LstdFlags)
		return logger, nil
	}
	var standardLogger = log.New(os.Stderr, "", log.LstdFlags)
	return standardLogger, nil

}
