package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

// var logger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)
// var logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

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

	debugHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logFile, err := os.OpenFile("linko.access.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	defer logFile.Close()

	infoHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	logger := slog.New(slog.NewMultiHandler(
		debugHandler,
		infoHandler,
	))
	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to create store: %v", err))
		return fmt.Errorf("failed created store")
	}
	s := newServer(*st, httpPort, cancel, logger)
	var serverErr error
	go func() {
		serverErr = s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error(fmt.Sprintf("failed to shutdown server: %v\n", err))
		return fmt.Errorf("error shutdown")
	}
	if serverErr != nil {
		logger.Error(fmt.Sprintf("server error: %v\n", serverErr))
		return fmt.Errorf("error server")
	}
	logger.Debug(fmt.Sprintf("Linko is shutting down"))
	return nil
}

func initializeLogger(logFile string) (*slog.Logger, CloseFunc, error) {

	if logFile != "" {

		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err

		}
		bufferedFile := bufio.NewWriterSize(f, 8192)
		multiWriter := io.MultiWriter(os.Stderr, bufferedFile)

		logger := slog.New(slog.NewTextHandler(multiWriter, nil))
		return logger, func() error {
			seg := bufferedFile.Flush()
			if seg != nil {
				return seg
			}
			return f.Close()

		}, nil
	}
	var standardLogger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	return standardLogger, func() error {
		return nil
	}, nil

}

type CloseFunc func() error
