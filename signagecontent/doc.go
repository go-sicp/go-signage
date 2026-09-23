// Package signagecontent is the optional central rendering server. It
// hosts a Content gRPC service that signage-agent puller clients query
// to retrieve their current frame.
//
// Frames are produced by a chromedp-driven Chromium that screenshots an
// operator-provided URL at the panel's resolution. The server caches the
// last PNG per device (keyed by device id) and short-circuits unchanged
// responses by returning a sentinel `unchanged=true` so the agent skips
// a needless panel refresh.
package signagecontent
