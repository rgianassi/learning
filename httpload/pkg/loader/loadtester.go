package loader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/aclements/go-moremath/stats"
)

// Results is a list of Result
type Results []Result

// Result represents a result from a request made by the load tester
type Result struct {
	status int
	timing time.Duration
}

// LoadTester represents an instance of the load testing logic
type LoadTester struct {
	results Results
	config  *Config
}

// NewLoadTesterFromConfig constructs a LoadTester instance given a Config
func NewLoadTesterFromConfig(config *Config) LoadTester {
	loadTester := LoadTester{}
	loadTester.config = config
	return loadTester
}

// DumpStatuses dumps response statuses contained in the Results to the given Writer
func (lt *LoadTester) DumpStatuses(w io.Writer) {
	statuses := make(map[int]int)

	for _, result := range lt.results {
		statuses[result.status]++
	}

	keys := make([]int, 0, len(statuses))

	for key := range statuses {
		keys = append(keys, key)
	}

	sort.Ints(keys)

	for _, key := range keys {
		code := float64(key)
		count := float64(statuses[key])
		fmt.Fprintf(w, "%s\n", fmt.Sprintf("[%3.f] %12.f response(s)", code, count))
	}
}

// DumpTimings dumps response timings contained in the Results to the given Writer
func (lt *LoadTester) DumpTimings(w io.Writer) {
	totalTiming := float64(0)
	maxTiming := float64(0)
	minTiming := float64(0)
	averageTiming := float64(0)
	requestsPerSecond := float64(0)

	n := len(lt.results)
	noResults := (n == 0)

	if noResults {
		return
	}

	timing0 := lt.results[0].timing.Seconds()
	totalTiming = timing0
	maxTiming = timing0
	minTiming = timing0

	for i := 1; i < n; i++ {
		timing := lt.results[i].timing.Seconds()

		totalTiming += timing

		if timing > maxTiming {
			maxTiming = timing
		}

		if timing < minTiming {
			minTiming = timing
		}
	}

	averageTiming = totalTiming / float64(n)
	requestsPerSecond = float64(1) / averageTiming

	fmt.Fprintf(w, "%s\n", fmt.Sprintf("Total:        %12.4f secs", totalTiming))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("Slowest:      %12.4f secs", maxTiming))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("Fastest:      %12.4f secs", minTiming))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("Average:      %12.4f secs", averageTiming))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("Requests/sec: %12.4f", requestsPerSecond))
}

// DumpDistribution dumps distribution percentiles contained in the Results to the given writer
func (lt *LoadTester) DumpDistribution(w io.Writer) {
	n := len(lt.results)

	sample := &stats.Sample{}
	sample.Xs = make([]float64, n)
	sample.Weights = make([]float64, n)

	for i := 0; i < n; i++ {
		timing := lt.results[i].timing.Seconds()
		sample.Xs = append(sample.Xs, timing)
		sample.Weights = append(sample.Weights, 1.0)
	}

	sortedSample := sample.Sort()

	fmt.Fprintf(w, "%s\n", fmt.Sprintf("10%% in %12.4f secs", sortedSample.Quantile(0.10)))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("25%% in %12.4f secs", sortedSample.Quantile(0.25)))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("50%% in %12.4f secs", sortedSample.Quantile(0.50)))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("75%% in %12.4f secs", sortedSample.Quantile(0.75)))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("90%% in %12.4f secs", sortedSample.Quantile(0.90)))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("95%% in %12.4f secs", sortedSample.Quantile(0.95)))
	fmt.Fprintf(w, "%s\n", fmt.Sprintf("99%% in %12.4f secs", sortedSample.Quantile(0.99)))
}

// WriteResults writes the results contained in this LoadTester to the given Writer
func (lt *LoadTester) WriteResults(w io.Writer) {
	fmt.Fprintf(w, "\n%s\n\n", "Summary:")
	lt.DumpTimings(w)

	fmt.Fprintf(w, "\n%s\n\n", "Status code distribution:")
	lt.DumpStatuses(w)

	fmt.Fprintf(w, "\n%s\n\n", "Request response times:")
	lt.DumpDistribution(w)
}

// ProcessRequest is a stage to make a single request, it generates a Result sent to the out channel
// see: https://medium.com/statuscode/pipeline-patterns-in-go-a37bb3a7e61d
func (lt *LoadTester) ProcessRequest(ctx context.Context, in <-chan string) (<-chan Result, <-chan error, error) {
	out := make(chan Result)
	errc := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errc)

		maxQPS := lt.config.MaxQPS()
		isRateLimiterOn := maxQPS > 0
		limiterTick := time.Duration(1)
		if isRateLimiterOn {
			limiterTick = time.Second / time.Duration(maxQPS)
		}
		limiter := time.Tick(limiterTick)

		for url := range in {
			if isRateLimiterOn {
				<-limiter
			}

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				errc <- err
				return
			}

			req = req.WithContext(ctx)

			start := time.Now()
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errc <- err
				return
			}
			defer resp.Body.Close()

			elapsed := time.Since(start)
			code := resp.StatusCode

			select {
			case out <- Result{code, elapsed}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, errc, nil
}

// RunRequestWorkers is a stage to launch N workers to process requests on the in channel, it collects Results on the out channel
// see: https://medium.com/statuscode/pipeline-patterns-in-go-a37bb3a7e61d
func (lt *LoadTester) RunRequestWorkers(ctx context.Context, in <-chan string) (<-chan Result, <-chan error, error) {
	out := make(chan Result)
	errc := make(chan error, 1)

	var wg sync.WaitGroup
	numWorkers := lt.config.NWorkers()
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()

			resultc, errcr, err := lt.ProcessRequest(ctx, in)
			if err != nil {
				errc <- err
				return
			}

			for result := range resultc {
				select {
				case out <- result:
				case <-ctx.Done():
					return
				}
			}
			for err := range errcr {
				select {
				case errc <- err:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()

		close(out)
		close(errc)
	}()

	return out, errc, nil
}

// AppendResults is a sink to save collected results in the in channel to the Results array
// see: https://medium.com/statuscode/pipeline-patterns-in-go-a37bb3a7e61d
func (lt *LoadTester) AppendResults(ctx context.Context, in <-chan Result) {
	for {
		select {
		case <-ctx.Done():
			return
		case result, ok := <-in:
			if !ok {
				return
			}
			lt.results = append(lt.results, result)
		}
	}
}

// WaitForPipeline waits for the pipeline to complete
// see: https://medium.com/statuscode/pipeline-patterns-in-go-a37bb3a7e61d
func (lt *LoadTester) WaitForPipeline(errs ...<-chan error) error {
	errc := lt.MergeErrors(errs...)
	for err := range errc {
		if err != nil {
			return err
		}
	}
	return nil
}

// MergeErrors merges errors coming from all pipeline stages
// see: https://medium.com/statuscode/pipeline-patterns-in-go-a37bb3a7e61d
func (lt *LoadTester) MergeErrors(cs ...<-chan error) <-chan error {
	var wg sync.WaitGroup

	// We must ensure that the output channel has the capacity to
	// hold as many errors as there are error channels.
	// This will ensure that it never blocks, even if WaitForPipeline returns early.
	out := make(chan error, len(cs))

	// Start an output goroutine for each input channel in cs. output
	// copies values from c to out until c is closed, then calls wg.Done.
	output := func(c <-chan error) {
		for n := range c {
			out <- n
		}
		wg.Done()
	}

	wg.Add(len(cs))
	for _, c := range cs {
		go output(c)
	}

	// Start a goroutine to close out once all the output goroutines are done.
	// This must start after the wg.Add call.
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// RunLoaderPipeline builds and executes the loader pipeline
// see: https://medium.com/statuscode/pipeline-patterns-in-go-a37bb3a7e61d
func (lt *LoadTester) RunLoaderPipeline(ctx context.Context) error {
	if lt.config.AppDuration() > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, lt.config.AppDuration())
		defer cancel()
	}

	var errcList []<-chan error
	urlc, errc, err := lt.config.RequestsSource(ctx)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	resultc, errc, err := lt.RunRequestWorkers(ctx, urlc)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	lt.AppendResults(ctx, resultc)
	err = lt.WaitForPipeline(errcList...)

	if errors.Is(err, context.DeadlineExceeded) {
		err = nil // DeadlineExceeded is not an error
	}

	return err
}
