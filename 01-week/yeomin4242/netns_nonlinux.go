//go:build !linux

package main

import "net/http"

func handleUnshareNetns(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "network namespace creation requires Linux", http.StatusNotImplemented)
}
