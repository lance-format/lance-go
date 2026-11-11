package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	lance "github.com/lance-format/lance-go"
	lancefile "github.com/lance-format/lance-go/file"
)

var workloadNames = []string{
	"open-close",
	"scan",
	"early-break",
	"cancel",
	"retain",
	"write",
	"rawfile",
	"blob",
}

type workloadStats struct {
	operations      atomic.Int64
	errors          atomic.Int64
	cancelObserved  atomic.Int64
	cancelCompleted atomic.Int64
}

type statsSet struct {
	byName map[string]*workloadStats
}

func newStatsSet(names []string) *statsSet {
	set := &statsSet{byName: make(map[string]*workloadStats, len(names))}
	for _, name := range names {
		set.byName[name] = &workloadStats{}
	}
	return set
}

type pool struct {
	starts []chan *statsSet
	dones  []chan struct{}
	stop   chan struct{}
	errors chan error
	wg     sync.WaitGroup
}

func newPool(ctx context.Context, cfg config, fx *fixture, names []string) *pool {
	p := &pool{
		starts: make([]chan *statsSet, cfg.Concurrency),
		dones:  make([]chan struct{}, cfg.Concurrency),
		stop:   make(chan struct{}),
		errors: make(chan error, cfg.Concurrency),
	}
	p.wg.Add(cfg.Concurrency)
	for i := 0; i < cfg.Concurrency; i++ {
		p.starts[i] = make(chan *statsSet)
		p.dones[i] = make(chan struct{})
		worker := &worker{
			id:    i,
			ctx:   ctx,
			cfg:   cfg,
			fx:    fx,
			names: names,
		}
		go worker.loop(p, p.starts[i], p.dones[i])
	}
	return p
}

// epoch starts exactly one operation on each worker and returns only after all
// of them have finished. Operations close their resources before reporting
// completion, so the caller observes a quiescent native and Go state.
func (p *pool) epoch(set *statsSet) {
	for _, start := range p.starts {
		start <- set
	}
	for _, done := range p.dones {
		<-done
	}
}

func (p *pool) firstErr() error {
	var first error
	for i := 0; i < cap(p.errors); i++ {
		select {
		case err := <-p.errors:
			if first == nil {
				first = err
			}
		default:
			return first
		}
	}
	return first
}

func (p *pool) close() {
	close(p.stop)
	p.wg.Wait()
}

type worker struct {
	id      int
	ctx     context.Context
	cfg     config
	fx      *fixture
	names   []string
	next    int
	current *workloadStats
}

func (w *worker) loop(p *pool, start <-chan *statsSet, done chan<- struct{}) {
	defer p.wg.Done()
	for {
		select {
		case <-p.stop:
			return
		case set := <-start:
			name := w.names[w.next]
			w.next = (w.next + 1) % len(w.names)
			stats := set.byName[name]
			w.current = stats
			stats.operations.Add(1)
			if err := w.invoke(name); err != nil {
				stats.errors.Add(1)
				select {
				case p.errors <- fmt.Errorf("worker %d workload %s: %w", w.id, name, err):
				default:
				}
			}
			w.current = nil
			done <- struct{}{}
		}
	}
}

func (w *worker) invoke(name string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	switch name {
	case "open-close":
		return w.openClose()
	case "scan":
		return w.scan()
	case "early-break":
		return w.earlyBreak()
	case "cancel":
		return w.cancel()
	case "retain":
		return w.retain()
	case "write":
		return w.write()
	case "rawfile":
		return w.rawFile()
	case "blob":
		return w.blob()
	default:
		return fmt.Errorf("unknown workload %q", name)
	}
}

func (w *worker) openClose() error {
	session, err := lance.NewSession(1<<20, 1<<20)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	dataset, err := lance.Open(w.ctx, w.fx.mainURI, lance.WithSession(session))
	if err != nil {
		_ = session.Close()
		return fmt.Errorf("open with session: %w", err)
	}
	rows, countErr := dataset.CountRows(w.ctx)
	datasetErr := dataset.Close()
	sessionErr := session.Close()
	if countErr != nil {
		return fmt.Errorf("count rows: %w", countErr)
	}
	if datasetErr != nil {
		return fmt.Errorf("close dataset: %w", datasetErr)
	}
	if sessionErr != nil {
		return fmt.Errorf("close session: %w", sessionErr)
	}
	if rows != uint64(w.fx.rows) {
		return fmt.Errorf("CountRows=%d, want %d", rows, w.fx.rows)
	}
	return nil
}

func (w *worker) scan() (err error) {
	dataset, err := lance.Open(w.ctx, w.fx.mainURI)
	if err != nil {
		return fmt.Errorf("open dataset: %w", err)
	}
	defer func() {
		if closeErr := dataset.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close dataset: %w", closeErr)
		}
	}()
	return w.fx.scanAndVerify(w.ctx, dataset, w.cfg.BatchSize, w.fx.rows, w.fx.expectedHash)
}

func (w *worker) earlyBreak() (err error) {
	dataset, err := lance.Open(w.ctx, w.fx.mainURI)
	if err != nil {
		return fmt.Errorf("open dataset: %w", err)
	}
	defer func() {
		if closeErr := dataset.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close dataset: %w", closeErr)
		}
	}()
	scanner, err := dataset.Scan(
		lance.WithColumns("id", "name", "payload"),
		lance.WithBatchSize(int64(w.cfg.BatchSize)),
	)
	if err != nil {
		return fmt.Errorf("create scan: %w", err)
	}
	received := false
	for batch, iterErr := range scanner.Records(w.ctx) {
		if iterErr != nil {
			return fmt.Errorf("scan records: %w", iterErr)
		}
		if _, _, err := w.fx.verifyBatch(batch); err != nil {
			return fmt.Errorf("verify first batch: %w", err)
		}
		received = true
		break
	}
	if closeErr := scanner.Close(); closeErr != nil {
		return fmt.Errorf("close scanner: %w", closeErr)
	}
	if !received {
		return errors.New("scan produced no batch before early break")
	}
	return nil
}

func (w *worker) cancel() (err error) {
	ctx, cancel := context.WithCancel(w.ctx)
	defer cancel()
	dataset, err := lance.Open(ctx, w.fx.mainURI)
	if err != nil {
		return fmt.Errorf("open dataset: %w", err)
	}
	defer func() {
		if closeErr := dataset.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close dataset: %w", closeErr)
		}
	}()
	scanner, err := dataset.Scan(
		lance.WithColumns("id", "name", "payload"),
		lance.WithBatchSize(int64(w.cfg.BatchSize)),
	)
	if err != nil {
		return fmt.Errorf("create scan: %w", err)
	}
	defer scanner.Close()

	delivered := 0
	var iterErr error
	for batch, scanErr := range scanner.Records(ctx) {
		if scanErr != nil {
			iterErr = scanErr
			break
		}
		if _, _, err := w.fx.verifyBatch(batch); err != nil {
			return fmt.Errorf("verify batch delivered before cancel: %w", err)
		}
		delivered++
		cancel()
	}
	if closeErr := scanner.Close(); closeErr != nil {
		return fmt.Errorf("close scanner: %w", closeErr)
	}
	if delivered == 0 {
		if iterErr != nil {
			return fmt.Errorf("scan before cancel: %w", iterErr)
		}
		return errors.New("cancel requested but the scan delivered no native batch")
	}
	if iterErr != nil {
		if !errors.Is(iterErr, context.Canceled) {
			return fmt.Errorf("scan after cancel: %w", iterErr)
		}
		w.current.cancelObserved.Add(1)
		return nil
	}
	// The scan may run out of buffered batches before it polls with the
	// canceled context. That is a valid outcome of a race, not a failure.
	w.current.cancelCompleted.Add(1)
	return nil
}

func (w *worker) retain() (err error) {
	dataset, err := lance.Open(w.ctx, w.fx.mainURI)
	if err != nil {
		return fmt.Errorf("open dataset: %w", err)
	}
	defer func() {
		if closeErr := dataset.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close dataset: %w", closeErr)
		}
	}()
	scanner, err := dataset.Scan(
		lance.WithColumns("id", "name", "payload"),
		lance.WithBatchSize(int64(w.cfg.BatchSize)),
	)
	if err != nil {
		return fmt.Errorf("create scan: %w", err)
	}
	var retained arrow.RecordBatch
	for batch, iterErr := range scanner.Records(w.ctx) {
		if iterErr != nil {
			return fmt.Errorf("scan records: %w", iterErr)
		}
		if _, _, err := w.fx.verifyBatch(batch); err != nil {
			return fmt.Errorf("verify batch before retain: %w", err)
		}
		batch.Retain()
		retained = batch
		break
	}
	if closeErr := scanner.Close(); closeErr != nil {
		if retained != nil {
			retained.Release()
		}
		return fmt.Errorf("close scanner: %w", closeErr)
	}
	if retained == nil {
		return errors.New("scan produced no batch to retain")
	}
	defer retained.Release()
	if _, _, err := w.fx.verifyBatch(retained); err != nil {
		return fmt.Errorf("verify retained batch after scanner close: %w", err)
	}
	return nil
}

func (w *worker) write() error {
	dir := filepath.Join(w.fx.writeDir, fmt.Sprintf("worker-%03d", w.id))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("reset write directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create write directory: %w", err)
	}
	uri := filepath.Join(dir, "data.lance")
	reader, err := array.NewRecordReader(w.fx.schema, []arrow.RecordBatch{w.fx.writeBatch})
	if err != nil {
		return fmt.Errorf("build write reader: %w", err)
	}
	dataset, err := lance.Create(w.ctx, uri, reader, lance.WithWriteParams(lance.WriteParams{EnableStableRowIDs: true}))
	reader.Release()
	if err != nil {
		return fmt.Errorf("create dataset: %w", err)
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("close created dataset: %w", err)
	}

	reopened, err := lance.Open(w.ctx, uri)
	if err != nil {
		return fmt.Errorf("reopen written dataset: %w", err)
	}
	verifyErr := w.fx.scanAndVerify(w.ctx, reopened, w.cfg.BatchSize, w.fx.writeRows, w.fx.writeHash)
	closeErr := reopened.Close()
	if verifyErr != nil {
		return fmt.Errorf("verify written dataset: %w", verifyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close reopened dataset: %w", closeErr)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clean write directory: %w", err)
	}
	return nil
}

func (w *worker) rawFile() (err error) {
	reader, err := lancefile.Open(w.ctx, w.fx.rawURI, nil)
	if err != nil {
		return fmt.Errorf("open raw file: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close raw file reader: %w", closeErr)
		}
	}()
	records, err := reader.ReadAll(w.ctx, lancefile.ReadOptions{BatchSize: uint32(w.cfg.BatchSize)})
	if err != nil {
		return fmt.Errorf("read raw file: %w", err)
	}
	defer records.Release()
	rows := 0
	var hash uint64
	for records.Next() {
		batchRows, batchHash, err := w.fx.verifyBatch(records.RecordBatch())
		if err != nil {
			return fmt.Errorf("verify raw file batch: %w", err)
		}
		rows += batchRows
		hash += batchHash
	}
	if err := records.Err(); err != nil {
		return fmt.Errorf("read raw file: %w", err)
	}
	if rows != w.fx.rows {
		return fmt.Errorf("raw file returned %d rows, want %d", rows, w.fx.rows)
	}
	if hash != w.fx.expectedHash {
		return fmt.Errorf("raw file hash %016x, want %016x", hash, w.fx.expectedHash)
	}
	return nil
}

func (w *worker) blob() (err error) {
	dataset, err := lance.Open(w.ctx, w.fx.blobURI)
	if err != nil {
		return fmt.Errorf("open blob dataset: %w", err)
	}
	defer func() {
		if closeErr := dataset.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close blob dataset: %w", closeErr)
		}
	}()
	reader, err := dataset.ReadBlobs(w.ctx, "blob", lance.BlobRowSelectorIndices, w.fx.blobIndices, nil)
	if err != nil {
		return fmt.Errorf("read blobs: %w", err)
	}
	var retained arrow.RecordBatch
	retainedOffset := 0
	rowOffset := 0
	for reader.Next() {
		batch := reader.RecordBatch()
		if retained == nil {
			batch.Retain()
			retained = batch
			retainedOffset = rowOffset
		}
		rowOffset += int(batch.NumRows())
	}
	streamErr := reader.Err()
	reader.Release()
	if streamErr != nil {
		if retained != nil {
			retained.Release()
		}
		return fmt.Errorf("blob stream: %w", streamErr)
	}
	if retained == nil {
		return errors.New("blob stream produced no batch")
	}
	defer retained.Release()
	if rowOffset != len(w.fx.blobIndices) {
		return fmt.Errorf("blob stream returned %d rows, want %d", rowOffset, len(w.fx.blobIndices))
	}
	if err := w.fx.verifyBlobBatch(retained, retainedOffset); err != nil {
		return fmt.Errorf("verify retained blob batch: %w", err)
	}
	return nil
}
