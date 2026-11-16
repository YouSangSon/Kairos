package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"omni-folio/services/core/internal/modules/trading/construction"
	"omni-folio/services/core/internal/modules/trading/contracts"
	"os"
	"syscall"
	"time"
)

type runner struct {
	resources construction.CLIResources
	input     *os.File
	output    io.Writer
}

func Run(ctx context.Context, command string, args []string, resources construction.CLIResources, input *os.File, output io.Writer) error {
	r := runner{resources: resources, input: input, output: output}
	switch command {
	case "paper-promote", "paper-promotion-status", "paper-promotion-abort":
		return r.runPaperPromotionCommand(ctx, command, args, output)
	case "paper-improve":
		return r.runPaperImprovementCommand(ctx, args, output)
	case "paper-input-status":
		return r.runPaperInputStatusCommand(ctx, args, output)
	case "paper-execute-stream":
		return r.runLocalPaperStreamCommand(ctx, args, input)
	case "paper-init", "paper-import-bars", "paper-execute":
		return r.runLocalPaperCommand(ctx, append([]string{command}, args...), output)
	default:
		return r.runStrategyCommand(ctx, append([]string{command}, args...))
	}
}

func (r runner) runLocalPaperCommand(ctx context.Context, args []string, output io.Writer) error {
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	dbPath := fs.String("db", "omni-folio.db", "existing migrated SQLite database")
	var account, selection, result, barsPath, proposalPath, researchPath, bundlePath string
	var arm bool
	if args[0] != "paper-import-bars" {
		fs.StringVar(&account, "account", "", "isolated paper account alias")
		fs.StringVar(&selection, "expected-current-event", "", "exact current selection event")
	}
	if args[0] == "paper-init" {
		fs.StringVar(&result, "result-sha256", "", "registered initial-capital research result")
	} else {
		fs.StringVar(&barsPath, "bars", "", "explicit-time paper fixture CSV")
	}
	if args[0] == "paper-execute" {
		fs.StringVar(&bundlePath, "bundle", "", "paper input bundle JSON; exclusive with separate input files")
		fs.StringVar(&proposalPath, "proposal", "", "offline paper proposal JSON")
		fs.StringVar(&researchPath, "research-bars", "", "original research CSV bound to the registered result")
		fs.BoolVar(&arm, "arm-paper", false, "explicitly arm this one-shot paper run; halt on exit")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || (args[0] != "paper-import-bars" && (account == "" || selection == "")) ||
		(args[0] == "paper-init" && result == "") || (args[0] == "paper-import-bars" && barsPath == "") ||
		(args[0] == "paper-execute" && (!arm ||
			(bundlePath == "" && (barsPath == "" || proposalPath == "" || researchPath == "")) ||
			(bundlePath != "" && (barsPath != "" || proposalPath != "" || researchPath != "")))) {
		return errors.New("local paper command requires all explicit inputs; paper-execute also requires -arm-paper")
	}
	var bars, proposal, research []byte
	var err error
	if bundlePath != "" {
		raw, readErr := readBoundedRegularFile(bundlePath, maxPaperBundleBytes)
		if readErr != nil {
			return errors.New("local paper bundle is unreadable or exceeds 4 MiB")
		}
		proposal, bars, research, err = decodePaperInputBundle(raw)
		if err != nil {
			return err
		}
	}
	if barsPath != "" {
		bars, err = readStrategyArtifact(barsPath)
		if err != nil {
			return errors.New("local paper CSV is unreadable or exceeds 1 MiB")
		}
	}
	if proposalPath != "" {
		proposal, err = readStrategyArtifact(proposalPath)
		if err != nil {
			return errors.New("local paper proposal is unreadable or exceeds 1 MiB")
		}
	}
	if researchPath != "" {
		research, err = readStrategyArtifact(researchPath)
		if err != nil {
			return errors.New("local paper research CSV is unreadable or exceeds 1 MiB")
		}
	}
	db, err := r.resources.OpenExisting(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.RequireSchema(); err != nil {
		return err
	}
	svc := db.Trading()
	var value any
	switch args[0] {
	case "paper-init":
		value, err = svc.InitializePaperAccount(ctx, account, result, selection)
	case "paper-import-bars":
		value, err = svc.ImportPaperSnapshot(ctx, bars)
	case "paper-execute":
		value, err = svc.ExecuteLocalPaper(ctx, account, selection, proposal, bars, research)
	default:
		return errors.New("unsupported local paper command")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(value)
}

func (r runner) runPaperPromotionCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	if ctx == nil {
		return errors.New("paper promotion command context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if command != "paper-promote" && command != "paper-promotion-status" && command != "paper-promotion-abort" {
		return errors.New("paper promotion command is invalid")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	attemptID := fs.String("attempt-id", "", "promotion attempt identity")
	var account, candidate, selection, barsPath string
	if command == "paper-promote" {
		fs.StringVar(&account, "account", "", "paper account reference")
		fs.StringVar(&candidate, "candidate-result-sha", "", "registered candidate result SHA-256")
		fs.StringVar(&selection, "expected-selection-event", "", "current selection event")
		fs.StringVar(&barsPath, "bars", "", "bounded forward market bars CSV")
	}
	if fs.Parse(args) != nil || fs.NArg() != 0 || *dbPath == "" || *attemptID == "" ||
		(command == "paper-promote" && (account == "" || candidate == "" || selection == "" || barsPath == "")) {
		return errors.New("paper promotion command arguments are invalid")
	}
	var raw []byte
	if command == "paper-promote" {
		var err error
		raw, err = readBoundedRegularFile(barsPath, maxMarketDataBytes)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("paper promotion bars must be a bounded readable regular file")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	var db construction.Session
	var err error
	if command == "paper-promotion-status" {
		db, err = r.resources.OpenReadOnly(ctx, *dbPath)
	} else {
		db, err = r.resources.OpenReadWrite(ctx, *dbPath)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return errors.New("paper promotion database is unavailable")
	}
	defer db.Close()
	svc := db.Trading()
	var receipt *contracts.PaperPromotionReceipt
	switch command {
	case "paper-promote":
		receipt, err = svc.PromotePaperCandidate(ctx, account, candidate, selection, *attemptID, raw)
	case "paper-promotion-status":
		receipt, err = svc.PaperPromotionStatus(ctx, *attemptID)
	case "paper-promotion-abort":
		receipt, err = svc.AbortPaperPromotion(ctx, *attemptID)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return errors.New("paper promotion operation failed; check status using the same attempt ID")
	}
	return json.NewEncoder(out).Encode(receipt)
}

func (r runner) runPaperImprovementCommand(ctx context.Context, args []string, out io.Writer) error {
	if ctx == nil {
		return errors.New("paper improvement command context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("paper-improve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	account := fs.String("account", "", "paper account reference")
	artifactPath := fs.String("artifact", "", "bounded strategy-improvement-result JSON")
	barsPath := fs.String("bars", "", "bounded forward market bars CSV")
	attemptID := fs.String("attempt-id", "", "comparison attempt identity")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *dbPath == "" || *account == "" || *artifactPath == "" || *barsPath == "" || *attemptID == "" {
		return errors.New("paper improvement command arguments are invalid")
	}
	artifact, err := readStrategyArtifact(*artifactPath)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("paper improvement artifact must be a bounded readable regular file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bars, err := readBoundedRegularFile(*barsPath, maxMarketDataBytes)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("paper improvement bars must be a bounded readable regular file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	failureMessage := "paper improvement database is unavailable"
	db, err := r.resources.OpenReadWrite(ctx, *dbPath)
	if err == nil {
		defer db.Close()
		var receipt *contracts.PaperImprovementReceipt
		receipt, err = db.Trading().ImprovePaperCandidate(ctx, *account, *attemptID, artifact, bars)
		if err == nil {
			return json.NewEncoder(out).Encode(receipt)
		}
		failureMessage = "paper improvement failed; registration or comparison may be saved; inspect strategy-status and inspect paper-promotion-status with the same attempt ID before retrying the same files and account"
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New(failureMessage)
}

func (r runner) runPaperInputStatusCommand(ctx context.Context, args []string, output io.Writer) error {
	if ctx == nil {
		return errors.New("paper input status context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("paper-input-status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "existing SQLite database")
	account := fs.String("account", "", "paper account reference")
	selection := fs.String("selection-event", "", "exact historical or current selection event")
	bundle := fs.String("bundle", "", "saved paper input bundle")
	if err := fs.Parse(args); err != nil {
		return errors.New("paper-input-status options are invalid")
	}
	if fs.NArg() != 0 || *dbPath == "" || *account == "" || *selection == "" || *bundle == "" {
		return errors.New("paper-input-status requires -db, -account, -selection-event and -bundle")
	}
	invalid := errors.New("paper input status could not be verified; input and database are unchanged")
	raw, err := readBoundedRegularFile(*bundle, maxPaperBundleBytes)
	if err != nil {
		return invalid
	}
	db, err := r.resources.OpenReadOnly(ctx, *dbPath)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return invalid
	}
	defer db.Close()
	result, err := db.Trading().PaperInputStatus(ctx, *account, *selection, raw)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return invalid
	}
	return json.NewEncoder(output).Encode(result)
}

func (r runner) runLocalPaperStreamCommand(ctx context.Context, args []string, source *os.File) error {
	defer source.Close() // Close original stdin as well, even on preflight failure.
	fs := flag.NewFlagSet("paper-execute-stream", flag.ContinueOnError)
	dbPath := fs.String("db", "omni-folio.db", "existing migrated SQLite database")
	account := fs.String("account", "", "isolated paper account alias")
	selection := fs.String("expected-current-event", "", "exact current selection event")
	arm := fs.Bool("arm-paper", false, "arm once for this invocation; halt on exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !*arm || !orderAlias(*account, "account") || !safeOrderID(*selection) {
		return errors.New("paper stream requires account, expected-current-event and -arm-paper")
	}
	info, err := source.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return errors.New("paper stream stdin must be a pipe")
	}
	// Inherited stdin may be blocking and unpollable. The command owns both
	// descriptors; nonblocking status is intentionally shared with original stdin.
	fd, err := syscall.Dup(int(source.Fd()))
	if err != nil {
		return errors.New("paper stream cannot duplicate input")
	}
	syscall.CloseOnExec(fd)
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return errors.New("paper stream cannot make input cancellable")
	}
	input := os.NewFile(uintptr(fd), "paper-stream-input")
	defer input.Close()
	if err := input.SetReadDeadline(time.Time{}); err != nil {
		return errors.New("paper stream requires a cancellable input pipe")
	}
	db, err := r.resources.OpenExisting(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.RequireSchema(); err != nil {
		return err
	}
	return runLocalPaperStream(ctx, db.Trading(), *account, *selection, input)
}

func runLocalPaperStream(ctx context.Context, service contracts.Trading, account, selection string, input *os.File) (resultErr error) {
	defer input.Close()
	if err := input.SetReadDeadline(time.Time{}); err != nil {
		return errors.New("paper stream requires a cancellable input pipe")
	}
	readCtx, cancel := context.WithCancel(ctx)
	type frame struct {
		raw []byte
		err error
	}
	frames := make(chan frame) // At most one pending copied frame; no unbounded queue.
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(frames)
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 4096), maxPaperBundleBytes+1)
		scanner.Split(splitPaperBundle)
		for scanner.Scan() {
			select {
			case frames <- frame{raw: bytes.Clone(scanner.Bytes())}:
			case <-readCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case frames <- frame{err: errors.New("paper stream input read or framing failed")}:
			case <-readCtx.Done():
			}
		}
	}()
	run := service.BeginLocalPaperRun(account, selection)
	defer func() {
		cancel()
		_ = input.Close() // Pollable Close interrupts a partial-frame read.
		<-done
		// Keep invocation cancellation observable across older redacted read
		// errors, without hiding either validation or independent cleanup errors.
		resultErr = errors.Join(resultErr, ctx.Err(), run.Close())
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := run.Heartbeat(ctx); err != nil {
				return err
			}
		case item, ok := <-frames:
			if err := ctx.Err(); err != nil {
				return err
			}
			if !ok {
				return nil
			}
			if item.err != nil {
				return item.err
			}
			result, err := run.AcceptBundle(ctx, item.raw)
			if err != nil {
				return err
			}
			if result.Policy.Decision == "HALT_AND_ROLLBACK" {
				return nil
			}
		}
	}
}

func (r runner) runStrategyCommand(ctx context.Context, args []string) error {
	switch args[0] {
	case "strategy-register":
		fs := flag.NewFlagSet("strategy-register", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		artifactPath := fs.String("artifact", "", "local strategy-improvement-result JSON path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *artifactPath == "" || fs.NArg() != 0 {
			return errors.New("strategy-register requires -artifact and no positional arguments")
		}
		artifact, err := readStrategyArtifact(*artifactPath)
		if err != nil {
			return err
		}
		db, err := r.resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.RequireSchema(); err != nil {
			return err
		}
		evidence, err := db.Trading().RegisterEvidence(context.Background(), artifact)
		if err != nil {
			return err
		}
		return json.NewEncoder(r.output).Encode(evidence)
	case "strategy-status":
		fs := flag.NewFlagSet("strategy-status", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("strategy-status accepts no positional arguments")
		}
		db, err := r.resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.RequireSchema(); err != nil {
			return err
		}
		state, err := db.Trading().StrategyStatus(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(r.output).Encode(state)

	case "strategy-select":
		fs := flag.NewFlagSet("strategy-select", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		resultSHA := fs.String("result-sha256", "", "registered paper_candidate result SHA-256")
		expected := fs.String("expected-current-event", "", "current selection event ID or no_event")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *resultSHA == "" || *expected == "" || fs.NArg() != 0 {
			return errors.New("strategy-select requires -result-sha256, -expected-current-event, and no positional arguments")
		}
		db, err := r.resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.RequireSchema(); err != nil {
			return err
		}
		state, err := db.Trading().SelectPaperCandidate(context.Background(), *resultSHA, *expected)
		if err != nil {
			return err
		}
		return json.NewEncoder(r.output).Encode(state)
	case "strategy-rollback":
		fs := flag.NewFlagSet("strategy-rollback", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		expected := fs.String("expected-current-event", "", "current selection event ID")
		source := fs.String("source-event", "", "selection event being rolled back")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *expected == "" || *source == "" || fs.NArg() != 0 {
			return errors.New("strategy-rollback requires -expected-current-event, -source-event, and no positional arguments")
		}
		db, err := r.resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.RequireSchema(); err != nil {
			return err
		}
		state, err := db.Trading().RollbackPaperCandidate(context.Background(), *expected, *source)
		if err != nil {
			return err
		}
		return json.NewEncoder(r.output).Encode(state)
	case "paper-run-due":
		fs := flag.NewFlagSet("paper-run-due", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		accountRef := fs.String("account", "", "paper account reference")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *accountRef == "" || fs.NArg() != 0 {
			return errors.New("paper-run-due requires -account and no positional arguments")
		}
		db, err := r.resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.RequireSchema(); err != nil {
			return err
		}
		result, err := db.Trading().RunDuePaperPerformancePolicy(context.Background(), *accountRef)
		if err != nil {
			return err
		}
		return json.NewEncoder(r.output).Encode(result)
	case "paper-run-loop":
		fs := flag.NewFlagSet("paper-run-loop", flag.ContinueOnError)
		dbPath := fs.String("db", "omni-folio.db", "SQLite database path")
		accountRef := fs.String("account", "", "paper account reference")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *accountRef == "" || fs.NArg() != 0 {
			return errors.New("paper-run-loop requires -account and no positional arguments")
		}
		db, err := r.resources.OpenExisting(*dbPath)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := db.RequireSchema(); err != nil {
			return err
		}
		return db.Trading().RunPaperPerformanceLoop(ctx, *accountRef)
	default:
		return fmt.Errorf("unknown trading command %q", args[0])
	}
}
