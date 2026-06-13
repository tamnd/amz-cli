package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/tamnd/amz-cli/amz"
)

func seedCmd(app *App) *cobra.Command {
	var file, entity string
	var priority int
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Enqueue ASINs/URLs into the crawl queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			c, err := app.Client()
			if err != nil {
				return err
			}
			lines, err := readSeeds(file, args)
			if err != nil {
				return exit(CodeUsage, err)
			}
			n := 0
			for _, line := range lines {
				url := seedURL(c, line, entity)
				if url == "" {
					continue
				}
				if err := s.Enqueue(cmd.Context(), url, entity, priority); err != nil {
					return exit(CodeRuntime, err)
				}
				n++
			}
			fmt.Fprintf(cmd.OutOrStdout(), "enqueued %d item(s)\n", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file of ASINs/URLs, one per line (- for stdin)")
	cmd.Flags().StringVar(&entity, "entity", amz.EntityProduct, "entity kind: product|reviews|qa|offers")
	cmd.Flags().IntVar(&priority, "priority", 0, "queue priority (higher drains first)")
	return cmd
}

func readSeeds(file string, args []string) ([]string, error) {
	var lines []string
	lines = append(lines, args...)
	if file == "" {
		if len(lines) == 0 {
			return nil, errors.New("provide --file or positional ASINs/URLs")
		}
		return lines, nil
	}
	var r *bufio.Scanner
	if file == "-" {
		r = bufio.NewScanner(os.Stdin)
	} else {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = bufio.NewScanner(f)
	}
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, r.Err()
}

func seedURL(c *amz.Client, line, entity string) string {
	asin := asinArg(line)
	switch entity {
	case amz.EntityReviews:
		return c.ReviewURL(asin, amz.ReviewQuery{}, 1)
	case amz.EntityQA:
		return c.QAURL(asin)
	case amz.EntityOffers:
		return c.OffersURL(asin)
	default:
		return c.ProductURL(asin)
	}
}

func enqueueSearch(cmd *cobra.Command, app *App, c *amz.Client, query string, q amz.SearchQuery) error {
	s, err := openStore(app)
	if err != nil {
		return err
	}
	n := 0
	ferr := c.Search(cmd.Context(), query, q, func(card amz.Card) error {
		if card.ASIN == "" {
			return nil
		}
		n++
		return s.Enqueue(cmd.Context(), c.ProductURL(card.ASIN), amz.EntityProduct, 0)
	})
	if ferr != nil {
		return exit(codeFor(ferr), ferr)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "enqueued %d product(s)\n", n)
	return nil
}

func crawlCmd(app *App) *cobra.Command {
	var kinds string
	cmd := &cobra.Command{
		Use:   "crawl",
		Short: "Drain the crawl queue into the local store",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore(app)
			if err != nil {
				return err
			}
			c, err := app.Client()
			if err != nil {
				return err
			}
			allow := map[string]bool{}
			for _, k := range splitCSV(kinds) {
				allow[k] = true
			}
			return drainQueue(cmd.Context(), cmd, app, s, c, allow)
		},
	}
	cmd.Flags().StringVar(&kinds, "kinds", "", "restrict entity kinds (comma-separated)")
	return cmd
}

func drainQueue(ctx context.Context, cmd *cobra.Command, app *App, s *amz.Store, c *amz.Client, allow map[string]bool) error {
	workers := app.Workers
	if workers < 1 {
		workers = 1
	}
	done, failed := 0, 0
	var mu sync.Mutex
	for {
		batch, err := s.NextBatch(ctx, workers*2)
		if err != nil {
			return exit(CodeRuntime, err)
		}
		if len(batch) == 0 {
			break
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, workers)
		for _, it := range batch {
			if len(allow) > 0 && !allow[it.Entity] {
				s.MarkStatus(ctx, it.ID, "skipped")
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(it amz.QueueItem) {
				defer wg.Done()
				defer func() { <-sem }()
				err := crawlOne(ctx, s, c, it)
				mu.Lock()
				if err != nil {
					failed++
					if errors.Is(err, amz.ErrBlocked) {
						s.MarkStatus(ctx, it.ID, "pending")
						fmt.Fprintln(cmd.ErrOrStderr(), "amz: blocked, backing off 60s")
						mu.Unlock()
						time.Sleep(60 * time.Second)
						return
					}
					s.MarkStatus(ctx, it.ID, "error")
				} else {
					done++
					s.MarkStatus(ctx, it.ID, "done")
				}
				mu.Unlock()
			}(it)
		}
		wg.Wait()
	}
	fmt.Fprintf(cmd.OutOrStdout(), "crawl complete: %d done, %d failed\n", done, failed)
	if done == 0 && failed > 0 {
		return exit(CodePartial, nil)
	}
	return nil
}

func crawlOne(ctx context.Context, s *amz.Store, c *amz.Client, it amz.QueueItem) error {
	asin := amz.ExtractASIN(it.URL)
	switch it.Entity {
	case amz.EntityReviews:
		return c.FetchReviews(ctx, asin, amz.ReviewQuery{Limit: 50}, func(r amz.Review) error {
			return s.PutReview(ctx, r)
		})
	case amz.EntityQA:
		err := c.FetchQA(ctx, asin, func(q amz.QA) error { return s.PutQA(ctx, q) })
		if errors.Is(err, amz.ErrNoQA) {
			return nil
		}
		return err
	default:
		p, err := c.FetchProduct(ctx, it.URL)
		if err != nil {
			return err
		}
		return s.PutProduct(ctx, p)
	}
}
