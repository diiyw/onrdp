//go:build (!linux || android || (!amd64 && !arm64)) && (!darwin || ios) && (!windows || (!amd64 && !arm64))

package app

import "onrdp/internal/rustdesk"

const rustDeskSupported = false

func newRustDeskClient(func(), rustdesk.Network) remoteClient { return nil }
