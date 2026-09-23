package signagecontent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Renderer screenshots a URL at the requested viewport. Implementations
// must be safe for concurrent calls from the gRPC layer.
type Renderer interface {
	RenderURL(ctx context.Context, url string, width, height int) ([]byte, error)
	Close() error
}

// NewChromedpRenderer spawns a single headless Chromium and returns a
// renderer that opens a fresh tab for each request. Closing the renderer
// cancels the allocator and shuts Chromium down.
func NewChromedpRenderer() (Renderer, error) {
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("hide-scrollbars", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
		)...,
	)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(browserCtx); err != nil {
		browserCancel()
		allocCancel()
		return nil, fmt.Errorf("chromedp warmup: %w", err)
	}
	return &chromedpRenderer{
		alloc:         allocCtx,
		browser:       browserCtx,
		allocCancel:   allocCancel,
		browserCancel: browserCancel,
	}, nil
}

type chromedpRenderer struct {
	alloc         context.Context
	browser       context.Context
	allocCancel   context.CancelFunc
	browserCancel context.CancelFunc

	mu sync.Mutex
}

func (r *chromedpRenderer) RenderURL(ctx context.Context, url string, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("renderer: width and height must be > 0")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tabCtx, cancel := chromedp.NewContext(r.browser)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(tabCtx, 30*time.Second)
	defer cancelTimeout()

	var png []byte
	err := chromedp.Run(ctx,
		chromedp.EmulateViewport(int64(w), int64(h)),
		chromedp.Navigate(url),
		chromedp.ActionFunc(func(ctx context.Context) error {
			data, err := page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				Do(ctx)
			if err != nil {
				return err
			}
			png = data
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("render %s: %w", url, err)
	}
	return png, nil
}

func (r *chromedpRenderer) Close() error {
	r.browserCancel()
	r.allocCancel()
	return nil
}
