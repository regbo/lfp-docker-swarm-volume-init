package main

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

const hostPathFilterEnv = "HOST_PATH_FILTER"

type config struct {
	hostPaths      hostPathFilter
	workerCount    int
	repairTimeout  time.Duration
	cleanupTimeout time.Duration
	reconnectMin   time.Duration
	reconnectMax   time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	filter, err := newHostPathFilter(getenv(hostPathFilterEnv))
	if err != nil {
		return config{}, err
	}

	return config{
		hostPaths:      filter,
		workerCount:    4,
		repairTimeout:  30 * time.Second,
		cleanupTimeout: 10 * time.Second,
		reconnectMin:   time.Second,
		reconnectMax:   30 * time.Second,
	}, nil
}

type hostPathFilter struct {
	roots []string
}

func newHostPathFilter(value string) (hostPathFilter, error) {
	var roots []string
	for root := range strings.SplitSeq(value, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		root = path.Clean(root)
		if !path.IsAbs(root) {
			return hostPathFilter{}, fmt.Errorf("%s entries must be absolute Linux paths: %q", hostPathFilterEnv, root)
		}
		if !slices.Contains(roots, root) {
			roots = append(roots, root)
		}
	}
	return hostPathFilter{roots: roots}, nil
}

func (f hostPathFilter) allows(value string) bool {
	if !path.IsAbs(value) {
		return false
	}
	cleaned := path.Clean(value)
	if len(f.roots) == 0 {
		return true
	}
	for _, root := range f.roots {
		if root == "/" || cleaned == root || strings.HasPrefix(cleaned, root+"/") {
			return true
		}
	}
	return false
}
