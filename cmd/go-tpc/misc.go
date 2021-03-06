package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pingcap/go-tpc/pkg/measurement"
	"github.com/pingcap/go-tpc/pkg/workload"
)

func checkPrepare(ctx context.Context, w workload.Workloader) {
	var wg sync.WaitGroup
	wg.Add(threads)
	for i := 0; i < threads; i++ {
		go func(index int) {
			defer wg.Done()

			ctx = w.InitThread(ctx, index)
			defer w.CleanupThread(ctx, index)

			if err := w.CheckPrepare(ctx, index); err != nil {
				fmt.Printf("check prepare failed, err %v\n", err)
				return
			}
		}(i)
	}
	wg.Wait()
}

func execute(ctx context.Context, w workload.Workloader, action string, index int) error {
	count := totalCount / threads

	ctx = w.InitThread(ctx, index)
	defer w.CleanupThread(ctx, index)

	switch action {
	case "prepare":
		if dropData {
			if err := w.Cleanup(ctx, index); err != nil {
				return err
			}
		}
		return w.Prepare(ctx, index)
	case "cleanup":
		return w.Cleanup(ctx, index)
	case "check":
		return w.Check(ctx, index)
	}

	for i := 0; i < count; i++ {
		err := w.Run(ctx, index)

		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if err != nil {
			// For TiDB, we may meet too many conflict errors, so here just ignore it
			if !silence && !strings.Contains(err.Error(), "conflict") {
				fmt.Printf("execute %s failed, err %v\n", action, err)
			}
			if !ignoreError {
				return err
			}
		}
	}

	return nil
}

func executeWorkload(ctx context.Context, w workload.Workloader, action string) {
	var wg sync.WaitGroup

	wg.Add(threads)

	outputCtx, outputCancel := context.WithCancel(ctx)
	ch := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(outputInterval)
		defer ticker.Stop()

		for {
			select {
			case <-outputCtx.Done():
				ch <- struct{}{}
				return
			case <-ticker.C:
				measurement.Output()
			}
		}
	}()

	for i := 0; i < threads; i++ {
		go func(index int) {
			defer wg.Done()
			if err := execute(ctx, w, action, index); err != nil {
				fmt.Printf("execute %s failed, err %v\n", action, err)
				return
			}
		}(i)
	}

	wg.Wait()

	if action == "prepare" {
		// For prepare, we must check the data consistency after all prepare finished
		checkPrepare(ctx, w)
	}
	outputCancel()

	<-ch

	fmt.Printf("Finished\n")
	measurement.Output()
}
