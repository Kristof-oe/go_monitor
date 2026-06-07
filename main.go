package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/linkoerr"
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

	logger, closeLogger, err := initializeLogger(os.Getenv("LINKO_LOG_FILE"))
	defer closeLogger()

	// debugHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
	// 	Level: slog.LevelDebug,
	// })

	logFile, err := os.OpenFile("linko.access.log", os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	defer logFile.Close()

	// infoHandler := slog.NewJSONHandler(logFile, &slog.HandlerOptions{
	// 	Level: slog.LevelInfo,
	// })

	// logger := slog.New(slog.NewMultiHandler(
	// 	debugHandler,
	// 	infoHandler,
	// ))
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

func initializeLogger(logFile string) (*slog.Logger, closeFunc, error) {
	handlers := []slog.Handler{
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
		}),
	}
	closers := []closeFunc{}

	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %w", err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)
		close := func() error {
			if err := bufferedFile.Flush(); err != nil {
				return fmt.Errorf("failed to flush log file: %w", err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("failed to close log file: %w", err)
			}
			return nil
		}
		handlers = append(handlers, slog.NewJSONHandler(bufferedFile, &slog.HandlerOptions{
			Level:       slog.LevelInfo,
			ReplaceAttr: replaceAttr,
		}))
		closers = append(closers, close)
	}
	closer := func() error {
		var errs []error
		for _, close := range closers {
			if err := close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	return slog.New(slog.NewMultiHandler(handlers...)), closer, nil
}

type closeFunc func() error

func replaceAttr(groups []string, a slog.Attr) slog.Attr {

	val := a.Value.Any()

	err, ok := val.(error)
	if !ok {
		return a
	}

	if a.Key == "error" {

		attrs := []slog.Attr{
			slog.Attr{Key: "message", Value: slog.StringValue(err.Error())},
		}
		extras := linkoerr.Attrs(err)
		attrs = append(attrs, extras...)
		if stackErr, ok := errors.AsType[stackTracer](err); ok {
			attrs = append(attrs, slog.Attr{
				Key:   "stack_trace",
				Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
			})
			// return slog.GroupAttrs("error", slog.Attr{
			// 	Key:   "message",
			// 	Value: slog.StringValue(stackErr.Error()),
			// }, slog.Attr{
			// 	Key:   "stack_trace",
			// 	Value: slog.StringValue(fmt.Sprintf("%+v", stackErr.StackTrace())),
			// })
		}
		return slog.GroupAttrs("error", attrs...)
	}
	return a
}
